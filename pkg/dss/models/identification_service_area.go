package models

import (
	"strconv"
	"time"

	"github.com/golang/geo/s2"
	"github.com/golang/protobuf/ptypes"
	dspb "github.com/steeling/InterUSS-Platform/pkg/dssproto"
)

type IdentificationServiceArea struct {
	// Embed the proto
	// Unfortunately some types don't implement scanner/valuer, so we add placeholders below.
	ID    string
	Url   string
	Owner string
	Cells s2.CellUnion
	// TODO(steeling): abstract NullTime away from models.
	StartTime  NullTime
	EndTime    NullTime
	UpdatedAt  time.Time
	AltitudeHi float32
	AltitudeLo float32
}

func (i *IdentificationServiceArea) Version() string {
	return timestampToVersionString(i.UpdatedAt)
}

func (i *IdentificationServiceArea) ToProto() (*dspb.IdentificationServiceArea, error) {
	result := &dspb.IdentificationServiceArea{
		Id:      i.ID,
		Owner:   i.Owner,
		Url:     i.Url,
		Version: strconv.FormatInt(i.UpdatedAt.UnixNano(), 10),
	}

	if i.StartTime.Valid {
		ts, err := ptypes.TimestampProto(i.StartTime.Time)
		if err != nil {
			return nil, err
		}
		result.StartTime = ts
	}

	if i.EndTime.Valid {
		ts, err := ptypes.TimestampProto(i.EndTime.Time)
		if err != nil {
			return nil, err
		}
		result.EndTime = ts
	}
	return result, nil
}
