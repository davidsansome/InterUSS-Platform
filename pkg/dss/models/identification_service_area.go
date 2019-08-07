package models

import (
	"context"
	"strconv"
	"time"

	"github.com/golang/geo/s2"
	"github.com/golang/protobuf/ptypes"
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"
)

type IdentificationServiceAreaStore interface {
	// Close closes the store and should release all resources.
	Close() error
	// Get returns the IdentificationServiceArea identified by "id".
	Get(ctx context.Context, id string, owner string) (*IdentificationServiceArea, []*Subscription, error)

	// Delete deletes the IdentificationServiceArea identified by "id" and owned by "owner".
	// Returns the delete IdentificationServiceArea and all Subscriptions affected by the delete.
	Delete(ctx context.Context, id string, owner string) (*IdentificationServiceArea, []*Subscription, error)

	Insert(ctx context.Context, isa *IdentificationServiceArea) (*IdentificationServiceArea, []*Subscription, error)

	Put(ctx context.Context, isa *IdentificationServiceArea) (*IdentificationServiceArea, []*Subscription, error)
	// SearchSubscriptions returns all subscriptions ownded by "owner" in "cells".
	Search(ctx context.Context, cells s2.CellUnion, owner string) ([]*IdentificationServiceArea, error)
}

type IdentificationServiceArea struct {
	// Embed the proto
	// Unfortunately some types don't implement scanner/valuer, so we add placeholders below.
	ID    string
	Url   string
	Owner string
	Cells s2.CellUnion
	// TODO(steeling): abstract nullTime away from models.
	BeginsAt   nullTime
	ExpiresAt  nullTime
	UpdatedAt  time.Time
	AltitudeHi float32
	AltitudeLo float32
}

func (i *IdentificationServiceArea) Version() string {
	return timestampToVersionString(i.UpdatedAt)
}

func (i *IdentificationServiceArea) ToProto() (*dspb.IdentificationServiceArea, error) {
	return nil, nil
}

func (i *IdentificationServiceArea) ToProto() (*dspb.IdentificationServiceArea, error) {
	result := &dspb.IdentificationServiceArea{
		Id:         i.ID,
		Owner:      i.Owner,
		FlightsUrl: i.Url,
		Version:    strconv.FormatInt(i.UpdatedAt.UnixNano(), 10),
	}

	ts, err := ptypes.TimestampProto(i.StartsAt)
	if err != nil {
		return nil, err
	}
	result.Extents = &dspb.Volume4D{
		TimeStart: ts,
	}

	ts, err = ptypes.TimestampProto(i.ExpiresAt)
	if err != nil {
		return nil, err
	}
	result.Extents.TimeEnd = ts

	return result, nil
}
