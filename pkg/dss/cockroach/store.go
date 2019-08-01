package cockroach

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"time"

	"github.com/golang/protobuf/ptypes"
	_ "github.com/lib/pq" // Pull in the postgres database driver
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"
	"go.uber.org/multierr"
)

type nullTime struct {
	Time  time.Time
	Valid bool
}

func (nt *nullTime) Scan(value interface{}) error {
	nt.Time, nt.Valid = value.(time.Time)
	return nil
}

func (nt nullTime) Value() (driver.Value, error) {
	if !nt.Valid {
		return nil, nil
	}
	return nt.Time, nil
}

type subscriptionsRow struct {
	id          string
	owner       string
	url         string
	typesFilter sql.NullString
	beginsAt    nullTime
	expiresAt   nullTime
	updatedAt   time.Time
}

func (sr *subscriptionsRow) scan(row *sql.Row) error {
	return row.Scan(&sr.id,
		&sr.owner,
		&sr.url,
		&sr.typesFilter,
		&sr.beginsAt,
		&sr.expiresAt,
		&sr.updatedAt,
	)
}

type subscriptionsStatusRow struct {
	subscriptionID    string
	notificationIndex int64
	lastUsedAt        nullTime
	updatedAt         time.Time
}

func (ssr *subscriptionsStatusRow) scan(row *sql.Row) error {
	return row.Scan(
		&ssr.subscriptionID,
		&ssr.notificationIndex,
		&ssr.lastUsedAt,
	)
}

// Store is an implementation of dss.Store using
// Cockroach DB as its backend store.
type Store struct {
	*sql.DB
}

// Close closes the underlying DB connection.
func (s *Store) Close() error {
	return s.DB.Close()
}

// insertSubscriptionUnchecked inserts subscription into the store and returns
// the resulting subscription including its ID.
//
// Please note that this function is only meant to be used in tests/benchmarks
// to bootstrap a store with values. In particular, the function is not
// validating timestamps of last updates.
func (s *Store) insertSubscriptionUnchecked(ctx context.Context, subscription *dspb.Subscription) (*dspb.Subscription, error) {
	const (
		subscriptionStatusQuery = `
		INSERT INTO
			subscriptions_status
		VALUES
			($1, $2, $3)
		RETURNING
			*
		`
		subscriptionQuery = `
		INSERT INTO
			subscriptions
		VALUES
			(gen_random_uuid(), $1, $2, $3, $4, $5, transaction_timestamp())
		RETURNING
			*`
	)

	sr := subscriptionsRow{
		owner: subscription.GetOwner(),
		url:   subscription.Callbacks.GetIdentificationServiceAreaUrl(),
	}

	if ts := subscription.GetBegins(); ts != nil {
		begins, err := ptypes.Timestamp(ts)
		if err != nil {
			return nil, err
		}
		sr.beginsAt.Time = begins
		sr.beginsAt.Valid = true
	}

	if ts := subscription.GetExpires(); ts != nil {
		expires, err := ptypes.Timestamp(ts)
		if err != nil {
			return nil, err
		}
		sr.expiresAt.Time = expires
		sr.expiresAt.Valid = true
	}

	tx, err := s.Begin()
	if err != nil {
		return nil, err
	}

	if err := sr.scan(tx.QueryRowContext(
		ctx,
		subscriptionQuery,
		sr.owner,
		sr.url,
		sr.typesFilter,
		sr.beginsAt,
		sr.expiresAt,
	)); err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	ssr := subscriptionsStatusRow{
		subscriptionID:    sr.id,
		notificationIndex: int64(subscription.NotificationIndex),
	}

	if err := ssr.scan(tx.QueryRowContext(
		ctx,
		subscriptionStatusQuery,
		ssr.subscriptionID,
		ssr.notificationIndex,
		ssr.lastUsedAt,
	)); err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	result := &dspb.Subscription{
		Id:    sr.id,
		Owner: sr.owner,
		Callbacks: &dspb.SubscriptionCallbacks{
			IdentificationServiceAreaUrl: sr.url,
		},
		NotificationIndex: int32(ssr.notificationIndex),
	}

	if sr.beginsAt.Valid {
		ts, err := ptypes.TimestampProto(sr.beginsAt.Time)
		if err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
		result.Begins = ts
	}

	if sr.expiresAt.Valid {
		ts, err := ptypes.TimestampProto(sr.expiresAt.Time)
		if err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
		result.Expires = ts
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, err
}

// DeleteSubscription deletes the subscription identified by "id" and
// returns the deleted subscription.
func (s *Store) DeleteSubscription(ctx context.Context, id string) (*dspb.Subscription, error) {
	const (
		subscriptionStatusQuery = `
		DELETE FROM
			subscriptions_status
		WHERE
			subscription_id = $1
		RETURNING
			*
		`
		subscriptionQuery = `
		DELETE FROM
			subscriptions
		WHERE
			id = $1
		RETURNING
			*`
	)

	tx, err := s.Begin()
	if err != nil {
		return nil, err
	}

	ssr := subscriptionsStatusRow{}

	if err := ssr.scan(tx.QueryRowContext(ctx, subscriptionStatusQuery, id)); err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	sr := subscriptionsRow{}

	if err := sr.scan(tx.QueryRowContext(ctx, subscriptionQuery, id)); err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	result := &dspb.Subscription{
		Id:    sr.id,
		Owner: sr.owner,
		Callbacks: &dspb.SubscriptionCallbacks{
			IdentificationServiceAreaUrl: sr.url,
		},
		NotificationIndex: int32(ssr.notificationIndex),
	}

	if sr.beginsAt.Valid {
		ts, err := ptypes.TimestampProto(sr.beginsAt.Time)
		if err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
		result.Begins = ts
	}

	if sr.expiresAt.Valid {
		ts, err := ptypes.TimestampProto(sr.expiresAt.Time)
		if err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
		result.Expires = ts
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, nil
}

// Bootstrap bootstraps the underlying database with required tables.
//
// TODO: We should handle database migrations properly, but bootstrap both us
// *and* the database with this manual approach here.
func (s *Store) Bootstrap(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS subscriptions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		owner STRING NOT NULL,
		url STRING NOT NULL,
		types_filter STRING,
		begins_at TIMESTAMPTZ,
		expires_at TIMESTAMPTZ,
		updated_at TIMESTAMPTZ NOT NULL,
		INDEX begins_at_idx (begins_at),
		INDEX expires_at_idx (expires_at)
	);
	CREATE TABLE IF NOT EXISTS subscriptions_status (
		subscription_id UUID NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
		notification_index INT8 DEFAULT 0,
		last_used_at TIMESTAMPTZ
	);
	CREATE TABLE IF NOT EXISTS cells_subscriptions (
		cell_id INT64 NOT NULL,
		cell_level INT CHECK (cell_level BETWEEN 0 and 30),
		subscription_id UUID NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
		updated_at TIMESTAMPTZ NOT NULL,
		PRIMARY KEY (cell_id, subscription_id),
		INDEX cell_id_idx (cell_id),
		INDEX subscription_id_idx (subscription_id)
	);
	CREATE TABLE IF NOT EXISTS identification_service_areas (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		owner STRING NOT NULL,
		url STRING NOT NULL,
		starts_at TIMESTAMPTZ NOT NULL,
		ends_at TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL,
		INDEX starts_at_idx (starts_at),
		INDEX ends_at_idx (ends_at)
	);
	CREATE TABLE IF NOT EXISTS cells_identification_service_areas (
		cell_id INT64 NOT NULL,
		cell_level INT CHECK (cell_level BETWEEN 0 and 30),
		identification_service_area_id UUID NOT NULL REFERENCES identification_service_areas (id) ON DELETE CASCADE,
		updated_at TIMESTAMPTZ NOT NULL,
		PRIMARY KEY (cell_id, identification_service_area_id),
		INDEX cell_id_idx (cell_id),
		INDEX identification_service_area_id_idx (identification_service_area_id)
	);
	`

	_, err := s.ExecContext(ctx, query)
	return err
}

// cleanUp drops all required tables from the store, useful for testing.
func (s *Store) cleanUp(ctx context.Context) error {
	const query = `
	DROP TABLE IF EXISTS subscriptions_status;
	DROP TABLE IF EXISTS cells_subscriptions;
	DROP TABLE IF EXISTS subscriptions;
	DROP TABLE IF EXISTS cells_identification_service_areas;
	DROP TABLE IF EXISTS identification_service_areas;`

	_, err := s.ExecContext(ctx, query)
	return err
}
