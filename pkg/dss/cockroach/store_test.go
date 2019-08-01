package cockroach

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"testing"

	"github.com/golang/protobuf/ptypes"
	"github.com/steeling/InterUSS-Platform/pkg/dss"
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"

	"github.com/stretchr/testify/require"
)

var (
	// Make sure that Store implements dss.Store.
	_ dss.Store = &Store{}

	storeURI = flag.String("store-uri", "", "URI pointing to a Cockroach node")
)

func init() {
	flag.Parse()
}

func setUpStore(ctx context.Context, t *testing.T) (*Store, func() error) {
	store, err := newStore()
	if err != nil {
		t.Skip(err)
	}
	require.NoError(t, store.Bootstrap(ctx))
	return store, func() error {
		return store.cleanUp(ctx)
	}
}

func newStore() (*Store, error) {
	if len(*storeURI) == 0 {
		return nil, errors.New("Missing command-line parameter store-uri")
	}

	db, err := sql.Open("postgres", *storeURI)
	if err != nil {
		return nil, err
	}

	return &Store{
		DB: db,
	}, nil
}

func TestStoreBootstrap(t *testing.T) {
	var (
		ctx                  = context.Background()
		store, tearDownStore = setUpStore(ctx, t)
	)
	require.NotNil(t, store)
	require.NoError(t, tearDownStore())
}

func TestStoreDeleteSubscription(t *testing.T) {
	var (
		ctx                  = context.Background()
		store, tearDownStore = setUpStore(ctx, t)
	)
	defer func() {
		require.NoError(t, tearDownStore())
	}()

	for _, r := range []struct {
		name  string
		input *dspb.Subscription
	}{
		{
			name: "a subscription without begins and expires",
			input: &dspb.Subscription{
				Owner: "me-myself-and-i",
				Callbacks: &dspb.SubscriptionCallbacks{
					IdentificationServiceAreaUrl: "https://no/place/like/home",
				},
				NotificationIndex: 42,
			},
		},
		{
			name: "a subscription with begins and expires",
			input: &dspb.Subscription{
				Owner: "me-myself-and-i",
				Callbacks: &dspb.SubscriptionCallbacks{
					IdentificationServiceAreaUrl: "https://no/place/like/home",
				},
				Begins:            ptypes.TimestampNow(),
				Expires:           ptypes.TimestampNow(),
				NotificationIndex: 42,
			},
		},
		{
			name: "a subscription with begins and without expires",
			input: &dspb.Subscription{
				Owner: "me-myself-and-i",
				Callbacks: &dspb.SubscriptionCallbacks{
					IdentificationServiceAreaUrl: "https://no/place/like/home",
				},
				Begins:            ptypes.TimestampNow(),
				NotificationIndex: 42,
			},
		},
		{
			name: "a subscription without begins and with expires",
			input: &dspb.Subscription{
				Owner: "me-myself-and-i",
				Callbacks: &dspb.SubscriptionCallbacks{
					IdentificationServiceAreaUrl: "https://no/place/like/home",
				},
				Expires:           ptypes.TimestampNow(),
				NotificationIndex: 42,
			},
		},
	} {
		t.Run(r.name, func(t *testing.T) {
			s1, err := store.insertSubscriptionUnchecked(ctx, r.input)
			require.NoError(t, err)
			require.NotNil(t, s1)

			s2, err := store.DeleteSubscription(ctx, s1.Id)
			require.NoError(t, err)
			require.NotNil(t, s2)

			require.Equal(t, *s1, *s2)
		})
	}
}
