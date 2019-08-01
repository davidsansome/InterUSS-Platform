package dss

import (
	"context"
	"errors"
	"testing"

	uuid "github.com/satori/go.uuid"
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockStore struct {
	mock.Mock
}

func (ms *mockStore) Close() error {
	return ms.Called().Error(0)
}

func (ms *mockStore) DeleteSubscription(ctx context.Context, id string) (*dspb.Subscription, error) {
	args := ms.Called(ctx, id)
	return args.Get(0).(*dspb.Subscription), args.Error(1)
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
		store := &mockStore{}
		store.On("Close").Return(error(nil))
		store.On("DeleteSubscription", mock.Anything, r.id).Return(
			r.subscription, r.err,
		)

		s := &Server{
			Store: store,
		}

		response, err := s.DeleteSubscription(context.Background(), &dspb.DeleteSubscriptionRequest{
			Id: r.id,
		})
		require.Equal(t, r.err, err)
		require.EqualValues(t, r.subscription, response.GetSubscription())
	}
}
