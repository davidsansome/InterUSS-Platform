package cockroach

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"testing"
	"time"

	"github.com/golang/geo/s2"
	"github.com/golang/protobuf/ptypes"
	uuid "github.com/satori/go.uuid"
	"github.com/steeling/InterUSS-Platform/pkg/dss"
	"github.com/steeling/InterUSS-Platform/pkg/dss/models"

	"github.com/stretchr/testify/require"
)

var (
	// Make sure that Store implements dss.Store.
	_ dss.Store = &Store{}

	storeURI  = flag.String("store-uri", "", "URI pointing to a Cockroach node")
	startTime = models.NullTime{Time: time.Now().AddDate(0, 0, -1), Valid: true}
	endTime   = models.NullTime{Time: time.Now().AddDate(0, 0, 1), Valid: true}
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

func TestDatabaseEnsuresBeginsBeforeExpires(t *testing.T) {
	var (
		ctx                  = context.Background()
		store, tearDownStore = setUpStore(ctx, t)
	)
	require.NotNil(t, store)
	defer func() {
		require.NoError(t, tearDownStore())
	}()

	var (
		begins  = time.Now()
		expires = begins.Add(-5 * time.Minute)
	)

	tsb, err := ptypes.TimestampProto(begins)
	require.NoError(t, err)
	tse, err := ptypes.TimestampProto(expires)
	require.NoError(t, err)

	_, err = store.insertSubscription(ctx, &dspb.Subscription{
		Id:    uuid.NewV4().String(),
		Owner: "me-myself-and-i",
		Callbacks: &dspb.SubscriptionCallbacks{
			IdentificationServiceAreaUrl: "https://no/place/like/home",
		},
		NotificationIndex: 42,
		Begins:            tsb,
		Expires:           tse,
	}, s2.CellUnion{})
	require.Error(t, err)
}
