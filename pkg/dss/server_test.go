package dss

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/geo/s2"
	uuid "github.com/satori/go.uuid"
	"github.com/steeling/InterUSS-Platform/pkg/dss/auth"
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"
	"github.com/steeling/InterUSS-Platform/pkg/logging"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockStore struct {
	mock.Mock
}

func (ms *mockStore) Close() error {
	return ms.Called().Error(0)
}

func (ms *mockStore) GetSubscription(ctx context.Context, id string) (*dspb.Subscription, error) {
	args := ms.Called(ctx, id)
	return args.Get(0).(*dspb.Subscription), args.Error(1)
}

func (ms *mockStore) DeleteSubscription(ctx context.Context, id string) (*dspb.Subscription, error) {
	args := ms.Called(ctx, id)
	return args.Get(0).(*dspb.Subscription), args.Error(1)
}

func (ms *mockStore) SearchSubscriptions(ctx context.Context, cells s2.CellUnion, owner string) ([]*dspb.Subscription, error) {
	args := ms.Called(ctx, cells, owner)
	return args.Get(0).([]*dspb.Subscription), args.Error(1)
}

func TestDeleteSubscriptionCallsIntoMockStore(t *testing.T) {
	for _, r := range []struct {
		name         string
		id           string
		subscription *dspb.Subscription
		err          error
	}{
		{
			name:         "subscription-is-returned-if-returned-from-store",
			id:           uuid.NewV4().String(),
			subscription: &dspb.Subscription{},
		},
		{
			name: "error-is-returned-if-returned-from-store",
			id:   uuid.NewV4().String(),
			err:  errors.New("failed to look up subscription for ID"),
		},
	} {
		t.Run(r.name, func(t *testing.T) {
			store := &mockStore{}
			store.On("DeleteSubscription", mock.Anything, r.id).Return(
				r.subscription, r.err,
			)
			s := &Server{
				Store:   DecorateLogging(logging.Logger, store),
				Coverer: DefaultRegionCoverer,
			}

			response, err := s.DeleteSubscription(context.Background(), &dspb.DeleteSubscriptionRequest{
				Id: r.id,
			})
			require.Equal(t, r.err, err)
			require.EqualValues(t, r.subscription, response.GetSubscription())
			require.True(t, store.AssertExpectations(t))
		})
	}
}

func TestGetSubscriptionCallsIntoMockStore(t *testing.T) {
	for _, r := range []struct {
		name         string
		id           string
		subscription *dspb.Subscription
		err          error
	}{
		{
			name:         "subscription-is-returned-if-returned-from-store",
			id:           uuid.NewV4().String(),
			subscription: &dspb.Subscription{},
		},
		{
			name: "error-is-returned-if-returned-from-store",
			id:   uuid.NewV4().String(),
			err:  errors.New("failed to look up subscription for ID"),
		},
	} {
		t.Run(r.name, func(t *testing.T) {
			store := &mockStore{}
			store.On("GetSubscription", mock.Anything, r.id).Return(
				r.subscription, r.err,
			)
			s := &Server{
				Store:   DecorateLogging(logging.Logger, store),
				Coverer: DefaultRegionCoverer,
			}

			response, err := s.GetSubscription(context.Background(), &dspb.GetSubscriptionRequest{
				Id: r.id,
			})
			require.Equal(t, r.err, err)
			require.EqualValues(t, r.subscription, response.GetSubscription())
			require.True(t, store.AssertExpectations(t))
		})
	}
}

func TestSearchSubscriptionsFailsIfOwnerMissingFromContext(t *testing.T) {
	var (
		ctx = context.Background()
		ms  = &mockStore{}
		s   = &Server{
			Store:   DecorateLogging(logging.Logger, &mockStore{}),
			Coverer: DefaultRegionCoverer,
			winding: windingCW,
		}
	)

	_, err := s.SearchSubscriptions(ctx, &dspb.SearchSubscriptionsRequest{
		Area: loop,
	})

	require.Error(t, err)
	require.True(t, ms.AssertExpectations(t))
}

func TestSearchSubscriptionsFailsForInvalidArea(t *testing.T) {
	var (
		ctx = auth.ContextWithOwner(context.Background(), "foo")
		ms  = &mockStore{}
		s   = &Server{
			Store:   DecorateLogging(logging.Logger, &mockStore{}),
			Coverer: DefaultRegionCoverer,
			winding: windingCW,
		}
	)

	_, err := s.SearchSubscriptions(ctx, &dspb.SearchSubscriptionsRequest{
		Area: loopWithOddNumberOfCoordinates,
	})

	require.Error(t, err)
	require.True(t, ms.AssertExpectations(t))
}

func TestSearchSubscriptionsCallsIntoStore(t *testing.T) {
	var (
		ctx = auth.ContextWithOwner(context.Background(), "foo")
		ms  = &mockStore{}
		s   = &Server{
			Store:   DecorateLogging(logging.Logger, ms),
			Coverer: DefaultRegionCoverer,
			winding: windingCW,
		}
	)

	ms.On("SearchSubscriptions", mock.Anything, mock.Anything, "foo").Return(
		[]*dspb.Subscription{
			{
				Id:    uuid.NewV4().String(),
				Owner: "me-myself-and-i",
				Callbacks: &dspb.SubscriptionCallbacks{
					IdentificationServiceAreaUrl: "https://no/place/like/home",
				},
				NotificationIndex: 42,
			},
		}, error(nil),
	)
	resp, err := s.SearchSubscriptions(ctx, &dspb.SearchSubscriptionsRequest{
		Area: loop,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Subscriptions, 1)
	require.True(t, ms.AssertExpectations(t))
}

func TestDefaultRegionCovererProducesResults(t *testing.T) {
	area, err := parseArea(loop, windingCW)
	require.NoError(t, err)
	require.NotNil(t, area)

	cells := DefaultRegionCoverer.Covering(area)
	require.NotNil(t, cells)
}
