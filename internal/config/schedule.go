package config

import "time"

// Due reports whether a profile is due relative to its last successful run.
// The caller supplies the location-aware clock; no wall-clock access occurs in
// this package.
func (p SearchProfile) Due(now time.Time, lastSuccess *time.Time) bool {
	if !p.IsEnabled() {
		return false
	}
	if lastSuccess == nil || p.Schedule == "always" {
		return true
	}
	last := lastSuccess.In(now.Location())
	if last.After(now) {
		return false
	}
	switch p.Schedule {
	case "daily":
		return now.YearDay() != last.YearDay() || now.Year() != last.Year()
	case "weekly":
		nowYear, nowWeek := now.ISOWeek()
		lastYear, lastWeek := last.ISOWeek()
		return nowYear != lastYear || nowWeek != lastWeek
	case "monthly":
		return now.Year() != last.Year() || now.Month() != last.Month()
	case "manual":
		return false
	default:
		return false
	}
}
