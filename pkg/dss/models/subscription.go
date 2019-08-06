package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/golang/geo/s2"
	"github.com/golang/protobuf/ptypes"
	"github.com/lib/pq"
)

type Subscription struct {
	// Embed the proto
	// Unfortunately some types don't implement scanner/valuer, so we add placeholders below.
	id                string
	url               string
	notificationIndex int
	owner             string
	cells             []s2.Cell
	loop              *s2.Loop
	beginsAt          nullTime
	expiresAt         nullTime
	updatedAt         nullTime
	altitude_hi       float
	altitude_lo       float
}

func SubscriptionFromProto() (*Subscription, error) {
	return nil, nil
}

// Apply s2 on top of s.
func (s *Subscription) inherit(s2 *Subscription) error {
	if s.id != s2.id {
		return errors.New("ids do not match")
	}
	if s.owner != s2.owner {
		return errors.New("owners do not match")
	}
	if s.url == "" {
		s.url = s2.url
	}
	if s.cells == nil {
		s.cells = s2.cells
	}
	if s.loop == nil {
		s.loop = s2.loop
	}
	if !s.beginsAt.Valid {
		s.beginsAt = s2.beginsAt
	}
	if !s.expiresAt.Valid {
		s.expiresAt = s2.expiresAt
	}
	if !s.updatedAt.Valid {
		s.updatedAt = s2.updatedAt
	}
	if s.altitude_hi == 0 {
		s.altitude_hi = s2.altitude_hi
	}
	if s.altitude_lo == 0 {
		s.altitude_lo = s2.altitude_lo
	}
	s.notificationIndex = old.notificationIndex
	return nil
}

func (s *Subscription) ToNotifyProto() *dspb.SubscriberToNotify {
	return &dspb.SubscriberToNotify{
		Url: s.url,
		Subscriptions: []*dspb.SubscriptionState{
			&dspb.SubscriptionState{
				NotificationIndex: s.notificationIndex,
				Subscription:      s.id,
			},
		},
	}
}

func (s *Subscription) scan(scanner scanner) error {
	err := scanner.Scan(
		&s.Id,
		&s.owner,
		&s.url,
		&sr.notificationIndex,
		&sr.beginsAt,
		&sr.expiresAt,
		&sr.updatedAt,
	)
	if err != nil {
		return err
	}
	// Populate the rest of the values here.
}

func (s *Subscription) Version() error {
	return timestampToVersionString(s.updatedAt)
}

func GetSubscription(ctx context.Context, tx *sql.Tx) (*Subscription, error) {
	const (
		query = `
		SELECT * FROM
			subscriptions
		WHERE
			id = $1`
	)
	s := new(Subscription)
	if err := s.scan(tx.QueryRowContext(ctx, query, id)); err != nil {
		return nil, err
	}
	return s, nil
}

func (sr *subscriptionsRow) ToProto() (*dspb.Subscription, error) {
	result := &dspb.Subscription{
		Id:    s.id,
		Owner: s.owner,
		Callbacks: &dspb.SubscriptionCallbacks{
			IdentificationServiceAreaUrl: s.url,
		},
		NotificationIndex: int32(s.notificationIndex),
		Version:           s.Version(),
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

//
func (s *Subscription) Insert(ctx context.Context, tx *sql.Tx) error {
	const query = `
		INSERT INTO
			subscriptions
		VALUES
			($1, $2, $3, $4, $5, $6, transaction_timestamp())
		RETURNING
			*`

	return s.scan(tx.QueryRowContext(
		ctx,
		query,
		s.id,
		s.owner,
		s.callback,
		0,
		s.beginsAt,
		s.expiresAt,
	))
}

//
func (s *Subscription) Update(ctx context.Context, tx *sql.Tx) error {
	const (
		// We use an upsert so we don't have to specify the column
		updateQuery = `
		UPSERT INTO
		  subscriptions
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, transaction_timestamp())
		RETURNING
			*`
		getQuery = `
		SELECT * FROM
			subscriptions
		WHERE
			id = $1`
	)

	old := &Subscription{}
	err := old.scan(tx.QueryRowContext(ctx, getQuery, s.id))
	if err != nil {
		return err
	}
	if s.Version() != "" && old.Version() != s.Version() {
		return fmt.Errorf("version mismatch for subscription %s", subscription.Id)
	}

	s.inherit(old)

	return s.scan(tx.QueryRowContext(
		ctx,
		updateQuery,
		s.id,
		s.owner,
		s.url,
		old.notificationIndex, // specifically use the value stored in the db.
		s.beginsAt,
		s.expiresAt,
	))
}

func (s *Subscription) Delete(ctx context.Context, tx *sql.Tx) error {
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
	switch s.Version() {
	case "":
		return sr.scan(tx.QueryRowContext(ctx, blindQuery, id))
	default:
		return sr.scan(tx.QueryRowContext(ctx, idempotentQuery, id, s.updatedAt))
	}
}

func Search(ctx context.Context, tx *sql.Tx, cells s2.CellUnion, owner string) ([]*Subscription, error) {
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

	rows, err := tx.QueryContext(ctx, subscriptionsInCellsQuery, pq.Array(cells), owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		row    = &Subscription{}
		result = []*dspb.Subscription{}
	)

	for rows.Next() {
		if err := row.scan(rows); err != nil {
			return nil, err
		}
		pb, err := row.ToProto()
		if err != nil {
			return nil, err
		}

		result = append(result, pb)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
