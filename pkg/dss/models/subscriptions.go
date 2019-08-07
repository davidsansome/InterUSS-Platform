package models

import (
	"context"
	"errors"

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
	Search(ctx context.Context, cells s2.CellUnion, owner string) (*Subscription, error)
}

// func GetSubscriptionStore() SubscriptionStore

var ActiveSubscriptionStore SubscriptionStore

type Subscription struct {
	// Embed the proto
	// Unfortunately some types don't implement scanner/valuer, so we add placeholders below.
	Id                string
	Url               string
	NotificationIndex int
	Owner             string
	Cells             s2.CellUnion
	// TODO(steeling): abstract nullTime away from models.
	BeginsAt   nullTime
	ExpiresAt  nullTime
	UpdatedAt  nullTime
	AltitudeHi float
	AltitudeLo float
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

func (s *Subscription) Version() error {
	return timestampToVersionString(s.updatedAt)
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
