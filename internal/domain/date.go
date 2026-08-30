package domain

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

const DateLayout = "2006-01-02"

// Date is a calendar date without a time zone. Snapshot dates are derived in
// Asia/Shanghai before being stored as Date values.
type Date string

var shanghaiLocation = mustLoadShanghaiLocation()

func mustLoadShanghaiLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(fmt.Sprintf("load Asia/Shanghai time zone: %v", err))
	}
	return location
}

func ShanghaiLocation() *time.Location {
	return shanghaiLocation
}

func ShanghaiDate(value time.Time) Date {
	return Date(value.In(shanghaiLocation).Format(DateLayout))
}

func ParseDate(value string) (Date, error) {
	parsed, err := time.ParseInLocation(DateLayout, value, shanghaiLocation)
	if err != nil || parsed.Format(DateLayout) != value {
		return "", fmt.Errorf("invalid date %q: expected YYYY-MM-DD", value)
	}
	return Date(value), nil
}

func (date Date) Validate() error {
	_, err := ParseDate(string(date))
	return err
}

func (date Date) Time() (time.Time, error) {
	if err := date.Validate(); err != nil {
		return time.Time{}, err
	}
	return time.ParseInLocation(DateLayout, string(date), shanghaiLocation)
}

func (date Date) AddDays(days int) (Date, error) {
	value, err := date.Time()
	if err != nil {
		return "", err
	}
	return Date(value.AddDate(0, 0, days).Format(DateLayout)), nil
}

func (date Date) String() string {
	return string(date)
}
