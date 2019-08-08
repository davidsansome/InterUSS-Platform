package cockroach

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang/geo/s2"
	"github.com/lib/pq"
	"github.com/steeling/InterUSS-Platform/pkg/dss/models"
	"go.uber.org/multierr"
)

func (c *Store) fetchISAs(ctx context.Context, q queryable, query string, args ...interface{}) ([]*models.IdentificationServiceArea, error) {
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
			&i.StartTime,
			&i.EndTime,
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

func (c *Store) fetchISAByID(ctx context.Context, q queryable, id string) (*models.IdentificationServiceArea, error) {
	// TODO(steeling) don't fetch by *
	const query = `
		SELECT * FROM
			identification_service_areas
		WHERE
			id = $1`
	i := new(models.IdentificationServiceArea)

	err := q.QueryRowContext(ctx, query, id).Scan(
		&i.ID,
		&i.Owner,
		&i.Url,
		&i.StartTime,
		&i.EndTime,
	)
	if err != nil {
		return nil, err
	}
	return i, nil
}

func (c *Store) fetchISAByIDAndOwner(ctx context.Context, q queryable, id, owner string) (*models.IdentificationServiceArea, error) {
	// TODO(steeling) don't fetch by *
	const query = `
		SELECT * FROM
			identification_service_areas
		WHERE
			id = $1
			AND owner = $2`
	i := new(models.IdentificationServiceArea)

	err := q.QueryRowContext(ctx, query, id, owner).Scan(
		&i.ID,
		&i.Owner,
		&i.Url,
		&i.StartTime,
		&i.EndTime,
	)
	if err != nil {
		return nil, err
	}
	return i, nil
}

func (c *Store) pushISA(ctx context.Context, tx *sql.Tx, i *models.IdentificationServiceArea) error {
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
		i.StartTime,
		i.EndTime,
	); err != nil {
		return err
	}

	// TODO(steeling) we also need to delete any leftover cells.
	for _, cell := range i.Cells {
		if _, err := tx.ExecContext(ctx, subscriptionCellQuery, cell, cell.Level(), i.ID); err != nil {
			return err
		}
	}
	return nil
}

func (c *Store) populateISACells(ctx context.Context, q queryable, i *models.IdentificationServiceArea) error {
	const query = `
	SELECT
		cell_id
	FROM
		cells_identification_service_areas
	WHERE identification_service_area_id = $1
		subscriptions.owner != $2`
	rows, err := q.QueryContext(ctx, query, i.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var cell s2.CellID
	i.Cells = s2.CellUnion{}
	for rows.Next() {
		if err := rows.Scan(&cell); err != nil {
			return err
		}
		i.Cells = append(i.Cells, cell)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

// PutIdentificationServiceArea creates the IdentificationServiceArea
// identified by "id" and owned by "owner", affecting "cells" in the time
// interval ["starts", "ends"].
//
// Returns the created/updated IdentificationServiceArea and all Subscriptions
// affected by the put.
func (c *Store) InsertISA(ctx context.Context, isa *models.IdentificationServiceArea) (*models.IdentificationServiceArea, []*models.Subscription, error) {
	const (
		subscriptionsQuery = `
		 SELECT
				subscriptions.*
			FROM
				subscriptions
			LEFT JOIN 
				(SELECT DISTINCT subscription_id FROM cells_subscriptions WHERE cell_id = ANY($1))
			AS
				unique_subscription_ids
			ON
				subscriptions.id = unique_subscription_ids.subscription_id
			WHERE
				subscriptions.owner != $2`
	)

	tx, err := c.Begin()
	if err != nil {
		return nil, nil, err
	}

	if err := c.pushISA(ctx, tx, isa); err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}

	// TODO(steeling) implement removing old cells
	subscriptions, err := c.fetchSubscriptions(ctx, tx, subscriptionsQuery, pq.Array(isa.Cells), isa.Owner)
	if err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	return isa, subscriptions, nil
}

// DeleteIdentificationServiceArea deletes the IdentificationServiceArea identified by "id" and owned by "owner".
// Returns the delete IdentificationServiceArea and all Subscriptions affected by the delete.
func (c *Store) DeleteISA(ctx context.Context, id string, owner, version string) (*models.IdentificationServiceArea, []*models.Subscription, error) {
	const (
		subscriptionsQuery = `
		 SELECT
				subscriptions.*
			FROM
				subscriptions
			LEFT JOIN 
				(SELECT DISTINCT subscription_id FROM cells_subscriptions WHERE cell_id = ANY($1))
			AS
				unique_subscription_ids
			ON
				subscriptions.id = unique_subscription_ids.subscription_id
			WHERE
				subscriptions.owner != $2`
		deleteQuery = `
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

	tx, err := c.Begin()
	if err != nil {
		return nil, nil, err
	}

	// We fetch to know whether to return a concurrency error, or a not found error
	old, err := c.fetchISAByIDAndOwner(ctx, tx, id, owner)
	switch {
	case err == sql.ErrNoRows: // Return a 404 here.
		return nil, nil, multierr.Combine(err, tx.Rollback())
	case err != nil:
		return nil, nil, multierr.Combine(err, tx.Rollback())
	case version != "" && version != old.Version():
		err := fmt.Errorf("version mismatch for subscription %s", id)
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}
	if err := c.populateISACells(ctx, tx, old); err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}

	subscriptions, err := c.fetchSubscriptions(ctx, tx, subscriptionsQuery, pq.Array(old.Cells), owner)
	if err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}

	if _, err := tx.ExecContext(ctx, deleteQuery, id, owner); err != nil {
		return nil, nil, multierr.Combine(err, tx.Rollback())
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	return old, subscriptions, nil
}

// SearchIdentificationServiceAreas searches IdentificationServiceArea
// instances that intersect with "cells" and, if set, the temporal volume
// defined by "earliest" and "latest".
func (c *Store) SearchISAs(ctx context.Context, cells s2.CellUnion, earliest *time.Time, latest *time.Time) ([]*models.IdentificationServiceArea, error) {
	const (
		serviceAreasInCellsQuery = `
			SELECT
				identification_service_areas.*
			FROM
				identification_service_areas
			JOIN 
				(SELECT DISTINCT
					cells_identification_service_areas.identification_service_area_id
				FROM
					cells_identification_service_areas
				WHERE
					cells_identification_service_areas.cell_id = ANY($1)
				)
			AS
				unique_identification_service_areas
			ON
				identification_service_areas.id = unique_identification_service_areas.identification_service_area_id
			WHERE
				COALESCE(identification_service_areas.starts_at >= $2, true)
			AND
				COALESCE(identification_service_areas.ends_at <= $3, true)`
	)

	if len(cells) == 0 {
		return nil, errors.New("missing cell IDs for query")
	}

	tx, err := c.Begin()
	if err != nil {
		return nil, err
	}

	result, err := c.fetchISAs(ctx, tx, serviceAreasInCellsQuery, pq.Array(cells), earliest, latest)
	if err != nil {
		return nil, multierr.Combine(err, tx.Rollback())
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Store) UpdateISA(ctx context.Context, isa *models.IdentificationServiceArea) (*models.IdentificationServiceArea, []*models.Subscription, error) {
	return nil, nil, nil
}
