package cockroach

import (
	"context"
	"testing"
	"time"

	"github.com/golang/geo/s2"
	"github.com/golang/protobuf/ptypes"
	uuid "github.com/satori/go.uuid"
	"github.com/steeling/InterUSS-Platform/pkg/dss/models"
	"github.com/stretchr/testify/require"
)

var (
	serviceAreasPool = []struct {
		name  string
		input *models.IdentificationServiceArea
	}{
		{
			name: "a subscription without startTime and endTime",
			input: &models.IdentificationServiceArea{
				ID:        uuid.NewV4().String(),
				Owner:     "me-myself-and-i",
				Url:       "https://no/place/like/home/for/flights",
				StartTime: startTime,
				EndTime:   endTime,
				Cells: s2.CellUnion{
					s2.CellID(42),
				},
			},
		},
	}
)

func TestStoreSearchIdentificationServiceAreas(t *testing.T) {
	var (
		ctx                  = context.Background()
		insertedServiceAreas = []*dspb.IdentificationServiceArea{}
		cells                = s2.CellUnion{
			s2.CellID(42),
			s2.CellID(84),
			s2.CellID(126),
			s2.CellID(168),
		}
		store, tearDownStore = setUpStore(ctx, t)
	)
	defer func() {
		require.NoError(t, tearDownStore())
	}()

	for _, r := range serviceAreasPool {
		saOut, err := store.insertIdentificationServiceAreaUnchecked(ctx, r.input, cells)
		require.NoError(t, err)
		require.NotNil(t, saOut)

		insertedServiceAreas = append(insertedServiceAreas, saOut)
	}

	for _, r := range []struct {
		name             string
		cells            s2.CellUnion
		timestampMutator func(time.Time, time.Time) (*time.Time, *time.Time)
		expectedLen      int
	}{
		{
			name:  "search for empty cell",
			cells: s2.CellUnion{s2.CellID(210)},
			timestampMutator: func(time.Time, time.Time) (*time.Time, *time.Time) {
				return nil, nil
			},
			expectedLen: 0,
		},
		{
			name:  "search for only one cell",
			cells: s2.CellUnion{s2.CellID(42)},
			timestampMutator: func(time.Time, time.Time) (*time.Time, *time.Time) {
				return nil, nil
			},
			expectedLen: 1,
		},
		{
			name:  "search with nil timestamps",
			cells: cells,
			timestampMutator: func(time.Time, time.Time) (*time.Time, *time.Time) {
				return nil, nil
			},
			expectedLen: 1,
		},
		{
			name:  "search with exact timestamps",
			cells: cells,
			timestampMutator: func(start time.Time, end time.Time) (*time.Time, *time.Time) {
				return &start, &end
			},
			expectedLen: 1,
		},
		{
			name:  "search with non-matching time span",
			cells: cells,
			timestampMutator: func(start time.Time, end time.Time) (*time.Time, *time.Time) {
				var (
					offset   = time.Duration(end.Sub(start).Seconds()/4) * time.Second
					earliest = start.Add(offset)
					latest   = end.Add(-offset)
				)

				return &earliest, &latest
			},
			expectedLen: 0,
		},
		{
			name:  "search with expanded time span",
			cells: cells,
			timestampMutator: func(start time.Time, end time.Time) (*time.Time, *time.Time) {
				var (
					offset   = time.Duration(end.Sub(start).Seconds()/4) * time.Second
					earliest = start.Add(-offset)
					latest   = end.Add(offset)
				)

				return &earliest, &latest
			},
			expectedLen: 1,
		},
	} {
		t.Run(r.name, func(t *testing.T) {
			for _, sa := range insertedServiceAreas {
				start, err := ptypes.Timestamp(sa.GetExtents().GetTimeStart())
				require.NoError(t, err)
				end, err := ptypes.Timestamp(sa.GetExtents().GetTimeEnd())
				require.NoError(t, err)

				earliest, latest := r.timestampMutator(start, end)

				serviceAreas, err := store.SearchIdentificationServiceAreas(ctx, r.cells, earliest, latest)
				require.NoError(t, err)
				require.Len(t, serviceAreas, r.expectedLen)
				for i := 0; i < r.expectedLen; i++ {
					require.Equal(t, sa.GetId(), serviceAreas[i].GetId())
				}
			}
		})
	}
}

func TestStoreDeleteIdentificationServiceAreas(t *testing.T) {
	var (
		ctx                  = context.Background()
		store, tearDownStore = setUpStore(ctx, t)
	)
	defer func() {
		require.NoError(t, tearDownStore())
	}()

	var (
		insertedServiceAreas  = []*models.IdentificationServiceArea{}
		insertedSubscriptions = []*models.Subscription{}
	)

	for _, r := range subscriptionsPool {
		s1, err := store.InsertSubscription(ctx, r.input)
		require.NoError(t, err)
		require.NotNil(t, s1)

		insertedSubscriptions = append(insertedSubscriptions, s1)
	}

	for _, r := range serviceAreasPool {
		tx, _ := store.Begin()
		err := store.pushISA(ctx, tx, r.input)
		tx.Commit()
		require.NoError(t, err)

		insertedServiceAreas = append(insertedServiceAreas, r.input)
	}

	for _, sa := range insertedServiceAreas {
		serviceAreaOut, subscriptionsOut, err := store.DeleteISA(ctx, sa.ID, sa.Owner, "")
		require.NoError(t, err)
		require.NotNil(t, serviceAreaOut)
		require.NotNil(t, subscriptionsOut)
	}
}

func (c *Store) UpdateISA(ctx context.Context, isa *models.IdentificationServiceArea) (*models.IdentificationServiceArea, []*models.Subscription, error) {
	return nil, nil, nil
}

// SearchSubscriptions returns all subscriptions ownded by "owner" in "cells".
func (c *Store) SearchISAs(ctx context.Context, cells s2.CellUnion, owner string) ([]*models.IdentificationServiceArea, error) {
	return nil, nil
}
