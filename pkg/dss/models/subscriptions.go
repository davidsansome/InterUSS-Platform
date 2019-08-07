package models

import (
	"context"
	"time"

	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"

	"github.com/golang/geo/s2"
	"github.com/golang/protobuf/ptypes"
)

type SubscriptionStore interface {
	// Close closes the store and should release all resources.
	Close() error
	// GetSubscription returns the subscription identified by "id".
	Get(ctx context.Context, id string) (*Subscription, error)

	// Delete deletes the subscription identified by "id" and
	// returns the deleted subscription.
	Delete(ctx context.Context, id, version string) (*Subscription, error)

	Insert(ctx context.Context, s *Subscription) (*Subscription, error)

	Put(ctx context.Context, s *Subscription) (*Subscription, error)

	// SearchSubscriptions returns all subscriptions ownded by "owner" in "cells".
	Search(ctx context.Context, cells s2.CellUnion, owner string) ([]*Subscription, error)
}

// func GetSubscriptionStore() SubscriptionStore

var ActiveSubscriptionStore SubscriptionStore

type Subscription struct {
	// Embed the proto
	// Unfortunately some types don't implement scanner/valuer, so we add placeholders below.
	ID                string
	Url               string
	NotificationIndex int
	Owner             string
	Cells             s2.CellUnion
	// TODO(steeling): abstract nullTime away from models.
	BeginsAt   nullTime
	ExpiresAt  nullTime
	UpdatedAt  time.Time
	AltitudeHi float32
	AltitudeLo float32
}

// Apply fields from s2 onto s, preferring any fields set in s2.
func (s *Subscription) Apply(s2 *Subscription) *Subscription {
	new := *s
	if s2.Url == "" {
		new.Url = s2.Url
	}
	if s2.Cells != nil {
		new.Cells = s2.Cells
	}
	if s2.BeginsAt.Valid {
		new.BeginsAt = s2.BeginsAt
	}
	if s2.ExpiresAt.Valid {
		new.ExpiresAt = s2.ExpiresAt
	}
	if !s2.UpdatedAt.IsZero() {
		new.UpdatedAt = s2.UpdatedAt
	}
	if s2.AltitudeHi != 0 {
		new.AltitudeHi = s2.AltitudeHi
	}
	// TODO(steeling) what if the update is to make it 0, we need an omitempty, pointer, or some other type.
	if s2.AltitudeLo != 0 {
		new.AltitudeLo = s2.AltitudeLo
	}
	return &new
}

func (s *Subscription) ToNotifyProto() *dspb.SubscriberToNotify {
	return &dspb.SubscriberToNotify{
		Url: s.Url,
		Subscriptions: []*dspb.SubscriptionState{
			&dspb.SubscriptionState{
				NotificationIndex: int32(s.NotificationIndex),
				Subscription:      s.ID,
			},
		},
	}
}

func (s *Subscription) Version() string {
	return timestampToVersionString(s.UpdatedAt)
}

func (s *Subscription) ToProto() (*dspb.Subscription, error) {
	result := &dspb.Subscription{
		Id:    s.ID,
		Owner: s.Owner,
		Callbacks: &dspb.SubscriptionCallbacks{
			IdentificationServiceAreaUrl: s.Url,
		},
		NotificationIndex: int32(s.NotificationIndex),
		Version:           s.Version(),
	}

	if s.BeginsAt.Valid {
		ts, err := ptypes.TimestampProto(s.BeginsAt.Time)
		if err != nil {
			return nil, err
		}
		result.Begins = ts
	}

	if s.ExpiresAt.Valid {
		ts, err := ptypes.TimestampProto(s.ExpiresAt.Time)
		if err != nil {
			return nil, err
		}
		result.Expires = ts
	}
	return result, nil
}
