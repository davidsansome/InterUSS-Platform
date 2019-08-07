package cockroach

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang/geo/s2"
	"github.com/golang/protobuf/ptypes"
	"github.com/lib/pq"
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"
	"go.uber.org/multierr"
)

type subscriptionsRow struct {
	id                string
	owner             string
	url               string
	typesFilter       sql.NullString
	notificationIndex int32
	lastUsedAt        pq.NullTime
	beginsAt          pq.NullTime
	expiresAt         pq.NullTime
	updatedAt         time.Time
}

func (sr *subscriptionsRow) scan(scanner scanner) error {
	return scanner.Scan(&sr.id,
		&sr.owner,
		&sr.url,
		&sr.typesFilter,
		&sr.notificationIndex,
		&sr.lastUsedAt,
		&sr.beginsAt,
		&sr.expiresAt,
		&sr.updatedAt,
	)
}

func (sr *subscriptionsRow) toProtobuf() (*dspb.Subscription, error) {
	result := &dspb.Subscription{
		Id:    sr.id,
		Owner: sr.owner,
		Callbacks: &dspb.SubscriptionCallbacks{
			IdentificationServiceAreaUrl: sr.url,
		},
		NotificationIndex: int32(sr.notificationIndex),
		Version:           timestampToVersionString(sr.updatedAt),
	}

	if sr.beginsAt.Valid {
		ts, err := ptypes.TimestampProto(sr.beginsAt.Time)
		if err != nil {
			return nil, err
		}
		result.Begins = ts
	}

	if sr.expiresAt.Valid {
		ts, err := ptypes.TimestampProto(sr.expiresAt.Time)
		if err != nil {
			return nil, err
		}
		result.Expires = ts
	}

	return result, nil
}

// from or apply
func (sr *subscriptionsRow) applyProtobuf(subscription *dspb.Subscription) error {
	if subscription.Id != "" {
		sr.id = subscription.Id
	}

	if subscription.Owner != "" {
		sr.owner = subscription.Owner
	}
	if subscription.Callbacks.GetIdentificationServiceAreaUrl() != "" {
		sr.url = subscription.Callbacks.GetIdentificationServiceAreaUrl()
	}
	if ts := subscription.GetBegins(); ts != nil {
		begins, err := ptypes.Timestamp(ts)
		if err != nil {
			return err
		}
		sr.beginsAt.Time = begins
		sr.beginsAt.Valid = true
	}

	if ts := subscription.GetExpires(); ts != nil {
		expires, err := ptypes.Timestamp(ts)
		if err != nil {
			return err
		}
		sr.expiresAt.Time = expires
		sr.expiresAt.Valid = true
	}
	return nil
}

// insertSubscription inserts subscription into the store and returns
// the resulting subscription including its ID.
func (s *Store) insertSubscription(ctx context.Context, subscription *dspb.Subscription, cells s2.CellUnion) (*dspb.Subscription, error) {
	const (
		insertQuery = `
		INSERT INTO
			subscriptions
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, transaction_timestamp())
		RETURNING
			*`
		subscriptionCellQuery = `
		INSERT INTO
			cells_subscriptions
		VALUES
			($1, $2, $3, transaction_timestamp())
		`
	)

	tx, err := s.Begin()
	if err != nil {
		return nil, err
	}

	sr := &subscriptionsRow{}

	sr.applyProtobuf(subscription)

	if err := sr.scan(tx.QueryRowContext(
		ctx,
		insertQuery,
		sr.id,
		sr.owner,
		sr.url,
		sr.typesFilter,
		sr.notificationIndex,
		sr.lastUsedAt,
		sr.beginsAt,
		sr.expiresAt,
	)); err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	for _, cell := range cells {
		if _, err := tx.ExecContext(ctx, subscriptionCellQuery, cell, cell.Level(), sr.id); err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
	}

	result, err := sr.toProtobuf()
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// updatesSubscription updates the subscription  and returns
// the resulting subscription including its ID.
func (s *Store) updateSubscription(ctx context.Context, subscription *dspb.Subscription, cells s2.CellUnion) (*dspb.Subscription, error) {
	const (
		// We use an upsert so we don't have to specify the column
		updateQuery = `
		UPSERT INTO
		  subscriptions
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, transaction_timestamp())
		RETURNING
			*`
		subscriptionCellQuery = `
		UPSERT INTO
			cells_subscriptions
		VALUES
			($1, $2, $3, transaction_timestamp())
		`
		getQuery = `
		SELECT * FROM
			subscriptions
		WHERE
			id = $1`
	)

	tx, err := s.Begin()
	if err != nil {
		return nil, err
	}

	sr := &subscriptionsRow{}

	err = sr.scan(tx.QueryRowContext(ctx, getQuery, subscription.Id))

	switch {
	case err == sql.ErrNoRows: // Do nothing here.
		return nil, multierr.Combine(err, tx.Rollback())
	case err != nil:
		return nil, multierr.Combine(err, tx.Rollback())
	case !sr.versionOK(subscription.Version):
		err := fmt.Errorf("version mismatch for subscription %s", subscription.Id)
		return nil, multierr.Combine(err, tx.Rollback())
	}

	sr.applyProtobuf(subscription)

	if err := sr.scan(tx.QueryRowContext(
		ctx,
		updateQuery,
		sr.id,
		sr.owner,
		sr.url,
		sr.typesFilter,
		sr.notificationIndex,
		sr.lastUsedAt,
		sr.beginsAt,
		sr.expiresAt,
	)); err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	// TODO(steeling) we also need to delete any leftover cells.
	for _, cell := range cells {
		if _, err := tx.ExecContext(ctx, subscriptionCellQuery, cell, cell.Level(), sr.id); err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
	}

	result, err := sr.toProtobuf()
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetSubscription returns the subscription identified by "id".
func (s *Store) GetSubscription(ctx context.Context, id string) (*dspb.Subscription, error) {
	const (
		subscriptionQuery = `
		SELECT * FROM
			subscriptions
		WHERE
			id = $1`
	)

	tx, err := s.Begin()
	if err != nil {
		return nil, err
	}

	sr := &subscriptionsRow{}

	if err := sr.scan(tx.QueryRowContext(ctx, subscriptionQuery, id)); err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	result, err := sr.toProtobuf()
	if err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, nil
}

// DeleteSubscription deletes the subscription identified by "id" and
// returns the deleted subscription.
func (s *Store) DeleteSubscription(ctx context.Context, id, version string) (*dspb.Subscription, error) {
	const (
		blindQuery = `
		DELETE FROM
			subscriptions
		WHERE
			id = $1
		RETURNING
			*`

		idempotentQuery = `
		DELETE FROM
			subscriptions
		WHERE
			id = $1
			AND updated_at = $2
		RETURNING
			*`
	)

	tx, err := s.Begin()
	if err != nil {
		return nil, err
	}

	sr := &subscriptionsRow{}
	switch version {
	case "":
		if err := sr.scan(tx.QueryRowContext(ctx, blindQuery, id)); err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
	default:
		updatedAt, err := versionStringToTimestamp(version)
		if err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
		if err := sr.scan(tx.QueryRowContext(ctx, idempotentQuery, id, updatedAt)); err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
	}

	result, err := sr.toProtobuf()
	if err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, nil
}

// SearchSubscriptions returns all subscriptions in "cells".
func (s *Store) SearchSubscriptions(ctx context.Context, cells s2.CellUnion, owner string) ([]*dspb.Subscription, error) {
	const (
		subscriptionsInCellsQuery = `
			SELECT
				subscriptions.*
			FROM
				subscriptions
			LEFT JOIN 
				(SELECT DISTINCT cells_subscriptions.subscription_id FROM cells_subscriptions WHERE cells_subscriptions.cell_id = ANY($1))
			AS
				unique_subscription_ids
			ON
				subscriptions.id = unique_subscription_ids.subscription_id
			WHERE
				subscriptions.owner = $2`
	)

	if len(cells) == 0 {
		return nil, errors.New("missing cell IDs for query")
	}

	tx, err := s.Begin()
	if err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, subscriptionsInCellsQuery, pq.Array(cells), owner)
	if err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}
	defer rows.Close()

	var (
		row    = &subscriptionsRow{}
		result = []*dspb.Subscription{}
	)

	for rows.Next() {
		if err := row.scan(rows); err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
		pb, err := row.toProtobuf()
		if err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}

		result = append(result, pb)
	}

	if err := rows.Err(); err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, nil
}
