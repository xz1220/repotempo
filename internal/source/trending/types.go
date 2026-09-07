// Package trending reads the public, all-language GitHub Trending lists.
// Page counts are discovery evidence only, never daily API Star snapshots.
package trending

import "time"

type Result struct {
	Windows []WindowResult `json:"windows"`
}

type WindowResult struct {
	Period     string    `json:"period"`
	URL        string    `json:"url"`
	CapturedAt time.Time `json:"captured_at"`
	Entries    []Entry   `json:"entries"`
	Error      string    `json:"error,omitempty"`
}

type Entry struct {
	FullName      string `json:"full_name"`
	Rank          int    `json:"rank"`
	StarsInPeriod *int64 `json:"stars_in_period,omitempty"`
	TotalStars    *int64 `json:"total_stars,omitempty"`
}
