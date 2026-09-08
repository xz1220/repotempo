package domain

// RadarCoverage counts the entire selected registry, including repositories
// that did not produce an observation on the selected date. Direction counts
// use exact endpoints; momentum requires three exact, successful observations.
type RadarCoverage struct {
	ComparisonCoverage
	FailedCount             int `json:"failed_count"`
	MissingCount            int `json:"missing_count"`
	StaleCount              int `json:"stale_count"`
	UpCount                 int `json:"up_count"`
	FlatCount               int `json:"flat_count"`
	DownCount               int `json:"down_count"`
	MomentumComparableCount int `json:"momentum_comparable_count"`
	SlowingCount            int `json:"slowing_count"`
}

// RadarHistoryPoint aggregates only the fixed cohort observed on both window
// endpoints. A missing member makes Stars and Index nil for that date, so a
// changing sample size never appears as market growth or decline. Index is
// 100 * Stars / baseline cohort Stars; a zero baseline leaves Index nil.
type RadarHistoryPoint struct {
	Date          Date     `json:"date"`
	Stars         *int64   `json:"stars,omitempty"`
	Index         *float64 `json:"index,omitempty"`
	ObservedCount int      `json:"observed_count"`
	CohortCount   int      `json:"cohort_count"`
}

type RadarOverview struct {
	AsOf         Date                    `json:"as_of"`
	BaselineDate Date                    `json:"baseline_date"`
	PreviousDate Date                    `json:"previous_date"`
	WindowDays   int                     `json:"window_days"`
	Coverage     RadarCoverage           `json:"coverage"`
	Fastest      []RepositoryTrendMetric `json:"fastest"`
	// Legacy overview fields remain empty for wire compatibility. The overview
	// only queries the positive-growth Top 10 and up to six slowing projects.
	Slowest         []RepositoryTrendMetric `json:"slowest"`
	FallingBehind   []RepositoryTrendMetric `json:"falling_behind"`
	NewRepositories []RepositoryTrendMetric `json:"new_repositories"`
	History         []RadarHistoryPoint     `json:"history"`
}
