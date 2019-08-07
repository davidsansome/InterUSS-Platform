package cockroach

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strconv"
	"time"

	"github.com/golang/protobuf/ptypes" // Pull in the postgres database driver
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"
)

type scanner interface {
	Scan(fields ...interface{}) error
}

type subscriberRow struct {
	id                string
	url               string
	notificationIndex int32
}

func (sr *subscriberRow) scan(scanner scanner) error {
	return scanner.Scan(
		&sr.url,
		&sr.notificationIndex,
	)
}

func (sr *subscriberRow) toProtobuf() (*dspb.SubscriberToNotify, error) {
	return &dspb.SubscriberToNotify{
		Url: sr.url,
		Subscriptions: []*dspb.SubscriptionState{
			&dspb.SubscriptionState{
				NotificationIndex: sr.notificationIndex,
				Subscription:      sr.id,
			},
		},
	}, nil
}

type identificationServiceAreaRow struct {
	id        string
	owner     string
	url       string
	startsAt  time.Time
	endsAt    time.Time
	updatedAt time.Time
}

func (isar *identificationServiceAreaRow) scan(scanner scanner) error {
	return scanner.Scan(
		&isar.id,
		&isar.owner,
		&isar.url,
		&isar.startsAt,
		&isar.endsAt,
		&isar.updatedAt,
	)
}

func (isar *identificationServiceAreaRow) toProtobuf() (*dspb.IdentificationServiceArea, error) {
	result := &dspb.IdentificationServiceArea{
		Id:         isar.id,
		Owner:      isar.owner,
		FlightsUrl: isar.url,
		Version:    strconv.FormatInt(isar.updatedAt.UnixNano(), 10),
	}

	ts, err := ptypes.TimestampProto(isar.startsAt)
	if err != nil {
		return nil, err
	}
	result.Extents = &dspb.Volume4D{
		TimeStart: ts,
	}

	ts, err = ptypes.TimestampProto(isar.endsAt)
	if err != nil {
		return nil, err
	}
	result.Extents.TimeEnd = ts

	return result, nil
}

// Convert updatedAt to a string, why not make it smaller
// WARNING: Changing this will cause RMW errors
// 32 is the highest value allowed by strconv
var versionBase = 32

// nullTime models a timestamp that could be NULL in the database. The model and
// implementation follows prior art as in sql.Null* types.
//
// Please note that this is rather driver-specific. The postgres sql driver
// errors out when trying to Scan a time.Time from a nil value. Other drivers
// might behave differently.
type nullTime struct {
	Time  time.Time
	Valid bool // Valid indicates whether Time carries a non-NULL value.
}

func (nt *nullTime) Scan(value interface{}) error {
	if value == nil {
		nt.Time = time.Time{}
		nt.Valid = false
		return nil
	}

	t, ok := value.(time.Time)
	if !ok {
		return fmt.Errorf("failed to cast database value, expected time.Time, got %T", value)
	}
	nt.Time, nt.Valid = t, ok

	return nil
}

func (nt nullTime) Value() (driver.Value, error) {
	if !nt.Valid {
		return nil, nil
	}
	return nt.Time, nil
}

func versionStringToTimestamp(s string) (time.Time, error) {
	var t time.Time
	nanos, err := strconv.ParseUint(s, versionBase, 64)
	if err != nil {
		return t, err
	}
	return time.Unix(0, int64(nanos)), nil
}

func timestampToVersionString(t time.Time) string {
	return strconv.FormatUint(uint64(t.UnixNano()), versionBase)
}

func (sr *subscriptionsRow) versionOK(version string) bool {
	return version == "" || version == timestampToVersionString(sr.updatedAt)
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

// Bootstrap bootstraps the underlying database with required tables.
//
// TODO: We should handle database migrations properly, but bootstrap both us
// *and* the database with this manual approach here.
func (s *Store) Bootstrap(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS subscriptions (
		id UUID PRIMARY KEY,
		owner STRING NOT NULL,
		url STRING NOT NULL,
		types_filter STRING,
		notification_index INT4 DEFAULT 0,
		last_used_at TIMESTAMPTZ,
		begins_at TIMESTAMPTZ,
		expires_at TIMESTAMPTZ,
		updated_at TIMESTAMPTZ NOT NULL,
		INDEX begins_at_idx (begins_at),
		INDEX expires_at_idx (expires_at),
		CHECK (begins_at IS NULL OR expires_at IS NULL OR begins_at < expires_at)
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
		id UUID PRIMARY KEY,
		owner STRING NOT NULL,
		url STRING NOT NULL,
		starts_at TIMESTAMPTZ NOT NULL,
		ends_at TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL,
		INDEX starts_at_idx (starts_at),
		INDEX ends_at_idx (ends_at),
		CHECK (starts_at IS NULL OR ends_at IS NULL OR starts_at < ends_at)
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
	DROP TABLE IF EXISTS cells_subscriptions;
	DROP TABLE IF EXISTS subscriptions;
	DROP TABLE IF EXISTS cells_identification_service_areas;
	DROP TABLE IF EXISTS identification_service_areas;`

	_, err := s.ExecContext(ctx, query)
	return err
}
