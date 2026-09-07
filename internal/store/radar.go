package store

import (
	"context"

	"github.com/xz1220/repotempo/internal/domain"
)

type RadarStore interface {
	RadarOverview(context.Context, domain.RepositoryTrendQuery) (domain.RadarOverview, error)
}

// PreferredSnapshotDateStore selects a completed collection batch as the
// default date, so an individual manual observation cannot move the whole
// dashboard to a mostly uncollected day.
type PreferredSnapshotDateStore interface {
	PreferredSnapshotDate(context.Context) (domain.Date, error)
}

// LatestLibraryDateStore includes newly registered projects before a snapshot
// succeeds, while allowing the dashboard to keep its completed-batch cutoff.
type LatestLibraryDateStore interface {
	LatestLibraryDate(context.Context) (domain.Date, error)
}
