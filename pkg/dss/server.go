package dss

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/golang/geo/s2"
	"github.com/golang/protobuf/ptypes"

	"github.com/steeling/InterUSS-Platform/pkg/dss/auth"
	"github.com/steeling/InterUSS-Platform/pkg/dss/geo"
	"github.com/steeling/InterUSS-Platform/pkg/dss/models"
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"
)

var (
	WriteISAScope = "dss.write.identification_service_areas"
	ReadISAScope  = "dss.read.identification_service_areas"
)

type SubscriptionStore interface {
	// Close closes the store and should release all resources.
	Close() error
	// GetSubscription returns the subscription identified by "id".
	Get(ctx context.Context, id string) (*models.Subscription, error)

	// Delete deletes the subscription identified by "id" and
	// returns the deleted subscription.
	Delete(ctx context.Context, id, version string) (*models.Subscription, error)

	Insert(ctx context.Context, s *models.Subscription) (*models.Subscription, error)

	Put(ctx context.Context, s *models.Subscription) (*models.Subscription, error)

	// SearchSubscriptions returns all subscriptions ownded by "owner" in "cells".
	Search(ctx context.Context, cells s2.CellUnion, owner string) (*models.Subscription, error)
}

type IdentificationServiceAreaStore interface {
	// Close closes the store and should release all resources.
	Close() error
	// Get returns the IdentificationServiceArea identified by "id".
	Get(ctx context.Context, id string, owner string) (*models.IdentificationServiceArea, []*models.Subscription, error)

	// Delete deletes the IdentificationServiceArea identified by "id" and owned by "owner".
	// Returns the delete IdentificationServiceArea and all Subscriptions affected by the delete.
	Delete(ctx context.Context, id string, owner string) (*models.IdentificationServiceArea, []*models.Subscription, error)

	Insert(ctx context.Context, isa *models.IdentificationServiceArea) (*models.IdentificationServiceArea, []*models.Subscription, error)

	Put(ctx context.Context, isa *models.IdentificationServiceArea) (*models.IdentificationServiceArea, []*models.Subscription, error)
	// SearchSubscriptions returns all subscriptions ownded by "owner" in "cells".
	Search(ctx context.Context, cells s2.CellUnion, owner string) ([]*models.IdentificationServiceArea, error)
}

// NewNilStore returns a nil Store instance.
func NewNilStore() Store {
	return nil
}

// Server implements dssproto.DiscoveryAndSynchronizationService.
type Server struct {
	*sql.DB
	scStore  models.SubscriptionStore
	isaStore models.IdentificationServiceAreaStore
}

func (s *Server) AuthScopes() map[string][]string {
	return map[string][]string{
		"GetIdentificationServiceArea":     []string{ReadISAScope},
		"PutIdentificationServiceArea":     []string{WriteISAScope},
		"PatchIdentificationServiceArea":   []string{WriteISAScope},
		"DeleteIdentificationServiceArea":  []string{WriteISAScope},
		"PutSubscription":                  []string{ReadISAScope},
		"PatchSubscription":                []string{ReadISAScope},
		"DeleteSubscription":               []string{ReadISAScope},
		"SearchSubscriptions":              []string{ReadISAScope},
		"SearchIdentificationServiceAreas": []string{ReadISAScope},
	}
}

func (s *Server) DeleteIdentificationServiceArea(ctx context.Context, req *dspb.DeleteIdentificationServiceAreaRequest) (*dspb.DeleteIdentificationServiceAreaResponse, error) {
	owner, ok := auth.OwnerFromContext(ctx)
	if !ok {
		// TODO(tvoss): Revisit once error propagation strategy is defined. We
		// might want to avoid leaking raw error messages to callers and instead
		// just return a generic error indicating a request ID.
		return nil, errors.New("missing owner from context")
	}

	isa, subscribers, err := s.isaStore.Delete(ctx, req.GetId(), owner)
	if err != nil {
		// TODO(tvoss): Revisit once error propagation strategy is defined. We
		// might want to avoid leaking raw error messages to callers and instead
		// just return a generic error indicating a request ID.
		return nil, err
	}

	p, err := isa.ToProto()
	if err != nil {
		return nil, err
	}
	sp := make([]*dspb.SubscriberToNotify, len(subscribers))
	for i, _ := range subscribers {
		sp[i], err = subscribers[i].ToNotifyProto()
		if err != nil {
			return nil, err
		}
	}

	return &dspb.DeleteIdentificationServiceAreaResponse{
		ServiceArea: p,
		Subscribers: sp,
	}, nil
}

func (s *Server) DeleteSubscription(ctx context.Context, req *dspb.DeleteSubscriptionRequest) (*dspb.DeleteSubscriptionResponse, error) {
	subscription, err := s.scStore.Delete(ctx, req.GetId(), req.GetVersion())
	if err != nil {
		// TODO(tvoss): Revisit once error propagation strategy is defined. We
		// might want to avoid leaking raw error messages to callers and instead
		// just return a generic error indicating a request ID.
		return nil, err
	}
	p, err := subscription.ToProto()
	if err != nil {
		return err
	}
	return &dspb.DeleteSubscriptionResponse{
		Subscription: p,
	}, nil
}

func (s *Server) SearchIdentificationServiceAreas(ctx context.Context, req *dspb.SearchIdentificationServiceAreasRequest) (*dspb.SearchIdentificationServiceAreasResponse, error) {
	cu, err := geo.AreaToCellIDs(req.GetArea())
	if err != nil {
		// TODO(tvoss): Revisit once error propagation strategy is defined. We
		// might want to avoid leaking raw error messages to callers and instead
		// just return a generic error indicating a request ID.
		return nil, err
	}

	var (
		earliest *time.Time
		latest   *time.Time
	)

	if et := req.GetEarliestTime(); et != nil {
		if ts, err := ptypes.Timestamp(et); err == nil {
			earliest = &ts
		} else {
			// TODO(tvoss): Revisit once error propagation strategy is defined. We
			// might want to avoid leaking raw error messages to callers and instead
			// just return a generic error indicating a request ID.
			return nil, err
		}
	}

	if lt := req.GetLatestTime(); lt != nil {
		if ts, err := ptypes.Timestamp(lt); err == nil {
			latest = &ts
		} else {
			// TODO(tvoss): Revisit once error propagation strategy is defined. We
			// might want to avoid leaking raw error messages to callers and instead
			// just return a generic error indicating a request ID.
			return nil, err
		}
	}

	serviceAreas, err := s.Store.SearchIdentificationServiceAreas(ctx, cu, earliest, latest)
	if err != nil {
		// TODO(tvoss): Revisit once error propagation strategy is defined. We
		// might want to avoid leaking raw error messages to callers and instead
		// just return a generic error indicating a request ID.
		return nil, err
	}

	return &dspb.SearchIdentificationServiceAreasResponse{
		ServiceAreas: serviceAreas,
	}, nil
}

func (s *Server) SearchSubscriptions(ctx context.Context, req *dspb.SearchSubscriptionsRequest) (*dspb.SearchSubscriptionsResponse, error) {
	owner, ok := auth.OwnerFromContext(ctx)
	if !ok {
		// TODO(tvoss): Revisit once error propagation strategy is defined. We
		// might want to avoid leaking raw error messages to callers and instead
		// just return a generic error indicating a request ID.
		return nil, errors.New("missing owner from context")
	}

	cu, err := geo.AreaToCellIDs(req.GetArea())
	if err != nil {
		return nil, err
	}

	subscriptions, err := s.scStore.Search(ctx, cu, owner)
	if err != nil {
		return nil, err
	}

	return &dspb.SearchSubscriptionsResponse{
		Subscriptions: subscriptions,
	}, nil
}

func (s *Server) GetSubscription(ctx context.Context, req *dspb.GetSubscriptionRequest) (*dspb.GetSubscriptionResponse, error) {
	subscription, err := s.scStore.Get(ctx, req.GetId())
	if err != nil {
		// TODO(tvoss): Revisit once error propagation strategy is defined. We
		// might want to avoid leaking raw error messages to callers and instead
		// just return a generic error indicating a request ID.
		return nil, err
	}
	p, err := subscription.ToProto()
	if err != nil {
		return err
	}
	return &dspb.GetSubscriptionResponse{
		Subscription: p,
	}, nil
}

func (s *Server) PatchIdentificationServiceArea(ctx context.Context, req *dspb.PatchIdentificationServiceAreaRequest) (*dspb.PatchIdentificationServiceAreaResponse, error) {
	return nil, nil
}

func (s *Server) PatchSubscription(ctx context.Context, req *dspb.PatchSubscriptionRequest) (*dspb.PatchSubscriptionResponse, error) {
	return nil, nil
}

func (s *Server) PutIdentificationServiceArea(ctx context.Context, req *dspb.PutIdentificationServiceAreaRequest) (*dspb.PutIdentificationServiceAreaResponse, error) {
	return nil, nil
}

func (s *Server) PutSubscription(ctx context.Context, req *dspb.PutSubscriptionRequest) (*dspb.PutSubscriptionResponse, error) {
	return nil, nil
}
