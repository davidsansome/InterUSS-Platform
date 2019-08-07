package cockroach

import (
	"context"
	"database/sql"

	"github.com/golang/geo/s2"
	"github.com/golang/protobuf/ptypes"
	"github.com/lib/pq"
	"github.com/steeling/InterUSS-Platform/pkg/dss/models"
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"
	"go.uber.org/multierr"
)

type crISAStore struct {
	*sql.DB
}

func (c *crISAStore) fetch(ctx context.Context, q queryable, query string, args ...interface{}) ([]*models.IdentificationServiceArea, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payload []*models.IdentificationServiceArea
	for rows.Next() {
		i := new(models.IdentificationServiceArea)

		err := rows.Scan(
			&i.ID,
			&i.Owner,
			&i.Url,
			&i.NotificationIndex,
			&i.BeginsAt,
			&i.ExpiresAt,
		)
		if err != nil {
			return nil, err
		}
		payload = append(payload, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *crISAStore) fetchByID(ctx context.Context, q queryable, id string) (*models.IdentificationServiceArea, error) {
	// TODO(steeling) don't fetch by *
	const query = `
		SELECT * FROM
			identification_service_areas
		WHERE
			id = $1`
	s := new(models.IdentificationServiceArea)

	err := q.QueryRowContext(ctx, query, id).Scan(
		&i.ID,
		&i.Owner,
		&i.Url,
		&i.NotificationIndex,
		&i.BeginsAt,
		&i.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	return i, nil
}

func (c *crSubscriptionStore) push(ctx context.Context, tx *sql.Tx, i *models.IdentificationServiceArea) error {
	const (
		upsertQuery = `
		UPSERT INTO
		  subscriptions
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, transaction_timestamp())`
		subscriptionCellQuery = `
		UPSERT INTO
			cells_subscriptions
		VALUES
			($1, $2, $3, transaction_timestamp())
		`
	)
	if _, err := tx.ExecContext(
		ctx,
		upsertQuery,
		i.ID,
		i.Owner,
		i.Url,
		i.NotificationIndex,
		i.BeginsAt,
		i.ExpiresAt,
	); err != nil {
		return err
	}

	// TODO(steeling) we also need to delete any leftover cells.
	for _, cell := range s.Cells {
		if _, err := tx.ExecContext(ctx, subscriptionCellQuery, cell, cell.Level(), s.ID); err != nil {
			return err
		}
	}
	return nil
}

func (c *crISAStore) Insert(ctx context.Context, serviceArea *dspb.IdentificationServiceArea, cells s2.CellUnion) (*dspb.IdentificationServiceArea, error) {
	const (
		subscriptionQuery = `
		INSERT INTO
			identification_service_areas
		VALUES
			($1, $2, $3, $4, $5, transaction_timestamp())
		RETURNING
			*`
		subscriptionCellQuery = `
		INSERT INTO
			cells_identification_service_areas
		VALUES
			($1, $2, $3, transaction_timestamp())
		`
	)

	isar := identificationServiceAreaRow{
		id:    serviceArea.GetId(),
		owner: serviceArea.GetOwner(),
		url:   serviceArea.GetFlightsUrl(),
	}

	starts, err := ptypes.Timestamp(serviceArea.GetExtents().GetTimeStart())
	if err != nil {
		return nil, err
	}
	isar.startsAt = starts

	ends, err := ptypes.Timestamp(serviceArea.GetExtents().GetTimeEnd())
	if err != nil {
		return nil, err
	}
	isar.endsAt = ends

	tx, err := s.Begin()
	if err != nil {
		return nil, err
	}

	if err := isar.scan(tx.QueryRowContext(
		ctx,
		subscriptionQuery,
		isar.id,
		isar.owner,
		isar.url,
		isar.startsAt,
		isar.endsAt,
	)); err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	for _, cell := range cells {
		if _, err := tx.ExecContext(ctx, subscriptionCellQuery, cell, cell.Level(), isar.id); err != nil {
			return nil, multierr.Combine(err, tx.Rollback())
		}
	}

	result := &dspb.IdentificationServiceArea{
		Id:         isar.id,
		Owner:      isar.owner,
		FlightsUrl: isar.url,
	}

	ts, err := ptypes.TimestampProto(isar.startsAt)
	if err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}
	result.Extents = &dspb.Volume4D{
		SpatialVolume: serviceArea.GetExtents().GetSpatialVolume(),
		TimeStart:     ts,
	}

	ts, err = ptypes.TimestampProto(isar.endsAt)
	if err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}
	result.Extents.TimeEnd = ts

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, err
}

// DeleteIdentificationServiceArea deletes the IdentificationServiceArea identified by "id" and owned by "owner".
// Returns the delete IdentificationServiceArea and all Subscriptions affected by the delete.
func (c *crISAStore) Delete(ctx context.Context, id string, owner string) (*dspb.IdentificationServiceArea, []*dspb.SubscriberToNotify, error) {
	const (
		getAffectedCellsAndSubscriptions = `
			SELECT
				cells_identification_service_areas.cell_id,
				affected_subscriptions.subscription_id
			FROM
				cells_identification_service_areas
			LEFT JOIN
				(SELECT DISTINCT cell_id, subscription_id FROM cells_subscriptions)
			AS
				affected_subscriptions
			ON
				affected_subscriptions.cell_id = cells_identification_service_areas.cell_id
			WHERE
				cells_identification_service_areas.identification_service_area_id = $1
		`
		getSubscriptionDetailsForAffectedCells = `
			SELECT
				id, url, notification_index
			FROM
				subscriptions
			WHERE
				id = ANY($1)
			AND
				owner != $2
			AND
				begins_at IS NULL OR transaction_timestamp() >= begins_at
			AND
				expires_at IS NULL OR transaction_timestamp() <= expires_at
		`
		deleteIdentificationServiceAreaQuery = `
			DELETE FROM
				identification_service_areas
			WHERE
				id = $1
			AND
				owner = $2
			RETURNING
				*
		`
	)

	tx, err := s.Begin()
	if err != nil {
		return nil, nil, err
	}

	var (
		cells         []int64
		subscriptions []string
		cell          int64
		subscription  string
	)

	rows, err := tx.QueryContext(ctx, getAffectedCellsAndSubscriptions, id)
	if err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}
	defer rows.Close()

	for rows.Next() {
		if err := rows.Scan(&cell, &subscription); err != nil {
			return nil, nil, multierr.Combine(err, tx.Rollback())
		}
		cells = append(cells, cell)
		subscriptions = append(subscriptions, subscription)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}

	isar := &identificationServiceAreaRow{}
	if err := isar.scan(tx.QueryRowContext(ctx, deleteIdentificationServiceAreaQuery, id, owner)); err != nil {
		// This error condition will be triggered if the owner does not match.
		multierr.Combine(err, tx.Rollback())
	}

	var (
		subscribers []*models.Subscription
		subscriber  *models.Subscription
	)

	rows, err = tx.QueryContext(ctx, getSubscriptionDetailsForAffectedCells, pq.Array(subscriptions), owner)
	if err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}
	defer rows.Close()

	for rows.Next() {
		if err := rows.Scan(&subscriber.id, &subscriber.url, &subscriber.notificationIndex); err != nil {
			return nil, nil, multierr.Combine(err, tx.Rollback())
		}

		subscribers = append(subscribers, subscriber)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}

	isa, err := isar.toProtobuf()
	if err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}

	subscribersToNotify := []*dspb.SubscriberToNotify{}
	for _, subscriber := range subscribers {
		subscriberToNotify, err := subscriber.toProtobuf()
		if err != nil {
			return nil, nil, multierr.Combine(err, tx.Rollback())
		}
		subscribersToNotify = append(subscribersToNotify, subscriberToNotify)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	return isa, subscribersToNotify, nil
}
