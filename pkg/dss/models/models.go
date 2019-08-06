package models

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Convert updatedAt to a string, why not make it smaller
// WARNING: Changing this will cause RMW errors
// 32 is the highest value allowed by strconv
var versionBase = 32

// Allows scanning row vs rows easily
type scanner interface {
	Scan(fields ...interface{}) error
}

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
