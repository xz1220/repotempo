package web

import (
	"context"
	"time"
)

// RadarQueryer is an optional boundary for the overview. Existing read-only
// Queryer implementations do not need to implement it.
type RadarQueryer interface {
	RadarOverview(context.Context, RepositoryQuery) (RadarOverview, error)
}

type RadarCoverage struct {
	ComparisonCoverage
	FailedCount             int
	MissingCount            int
	StaleCount              int
	UpCount                 int
	FlatCount               int
	DownCount               int
	MomentumComparableCount int
	SlowingCount            int
}

type RadarRepository struct {
	RepositoryMetric
	LastObservedAt    *time.Time
	LastObservedStars *int64
	IsStale           bool
	PreviousDelta     *int64
	MomentumChange    *int64
	GitHubCreatedAt   *time.Time
}

type RadarHistoryPoint struct {
	Date          time.Time
	Stars         *int64
	Index         *float64
	ObservedCount int
	CohortCount   int
}

type RadarOverview struct {
	Filter          RepositoryQuery
	AsOf            time.Time
	BaselineDate    time.Time
	PreviousDate    time.Time
	Coverage        RadarCoverage
	Fastest         []RadarRepository
	Slowest         []RadarRepository
	FallingBehind   []RadarRepository
	NewRepositories []RadarRepository
	History         []RadarHistoryPoint
	Topics          []TopicRef
}
