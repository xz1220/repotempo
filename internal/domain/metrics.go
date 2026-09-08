package domain

// GrowthMetric never interpolates an observation. Current requires a successful
// value on the query date. Every delta requires successful values on that exact
// endpoint and its exact 1, 7, or 30-day boundary. Missing or failed endpoints
// therefore remain nil instead of becoming a wider, mislabeled comparison.
type GrowthMetric struct {
	Current   *int64 `json:"current"`
	Day       *int64 `json:"day"`
	SevenDay  *int64 `json:"seven_day"`
	ThirtyDay *int64 `json:"thirty_day"`
}

type CoverageMetric struct {
	Date         Date    `json:"date"`
	TargetCount  int     `json:"target_count"`
	SuccessCount int     `json:"success_count"`
	FailureCount int     `json:"failure_count"`
	MissingCount int     `json:"missing_count"`
	Percent      float64 `json:"percent"`
}

type RepositoryMetric struct {
	Repository Repository          `json:"repository"`
	Growth     GrowthMetric        `json:"growth"`
	Topics     []Topic             `json:"topics"`
	Analysis   *RepositoryAnalysis `json:"analysis,omitempty"`
}

type RepositorySort string

const (
	RepositorySortName      RepositorySort = "name"
	RepositorySortStars     RepositorySort = "stars"
	RepositorySortDay       RepositorySort = "day"
	RepositorySortSevenDay  RepositorySort = "seven_day"
	RepositorySortThirtyDay RepositorySort = "thirty_day"
)

type RepositoryMetricQuery struct {
	AsOf             Date
	RepositoryID     *int64
	Search           string
	TopicSlug        string
	DiscoverySource  DiscoverySource
	MonitoringStatus MonitoringStatus
	GitHubStatus     GitHubStatus
	Sort             RepositorySort
	Descending       bool
	Limit            int
	Offset           int
}

// RepositoryTrendSort controls the primary ordering of the monitoring view.
// Every ordering is stabilized by observed stars and the immutable repository
// ID. LowGrowth and Slowdown also restrict the list to their measured cohorts.
type RepositoryTrendSort string

const (
	RepositoryTrendSortRankChange RepositoryTrendSort = "rank_change"
	RepositoryTrendSortStars      RepositoryTrendSort = "stars"
	RepositoryTrendSortDelta      RepositoryTrendSort = "delta"
	RepositoryTrendSortGrowthRate RepositoryTrendSort = "growth_rate"
	RepositoryTrendSortVelocity   RepositoryTrendSort = "velocity"
	RepositoryTrendSortLowGrowth  RepositoryTrendSort = "low_growth"
	RepositoryTrendSortSlowdown   RepositoryTrendSort = "slowdown"
	RepositoryTrendSortNewest     RepositoryTrendSort = "newest"
)

// RepositoryTrendQuery compares two strict calendar endpoints. A repository
// is comparable only when both dates have successful observations. AfterID is
// an opaque keyset cursor resolved inside SQLite, so the caller never embeds
// metric values in a URL.
type RepositoryTrendQuery struct {
	AsOf             Date
	WindowDays       int
	Search           string
	TopicSlug        string
	Tag              string
	DiscoverySource  DiscoverySource
	MonitoringStatus MonitoringStatus
	Sort             RepositoryTrendSort
	OnlyNew          bool
	OnlyFocus        bool
	Limit            int
	AfterID          *int64
}

// RepositoryTag is one exact, normalized label and its repository count in the
// selected historical registry. Labels do not create taxonomy assignments.
type RepositoryTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type ComparisonCoverage struct {
	BaselineDate    Date `json:"baseline_date"`
	AsOfDate        Date `json:"as_of_date"`
	ScopeCount      int  `json:"scope_count"`
	ObservedCount   int  `json:"observed_count"`
	ComparableCount int  `json:"comparable_count"`
	NewCount        int  `json:"new_count"`
}

type RepositoryTrendMetric struct {
	Repository        Repository          `json:"repository"`
	Topics            []Topic             `json:"topics"`
	Analysis          *RepositoryAnalysis `json:"analysis,omitempty"`
	CurrentStars      *int64              `json:"current_stars,omitempty"`
	BaselineStars     *int64              `json:"baseline_stars,omitempty"`
	CurrentRank       *int64              `json:"current_rank,omitempty"`
	BaselineRank      *int64              `json:"baseline_rank,omitempty"`
	RankChange        *int64              `json:"rank_change,omitempty"`
	StarDelta         *int64              `json:"star_delta,omitempty"`
	GrowthRate        *float64            `json:"growth_rate,omitempty"`
	DailyVelocity     *float64            `json:"daily_velocity,omitempty"`
	IsNew             bool                `json:"is_new"`
	LastObservedDate  *Date               `json:"last_observed_date,omitempty"`
	LastObservedStars *int64              `json:"last_observed_stars,omitempty"`
	IsStale           bool                `json:"is_stale"`
	PreviousDelta     *int64              `json:"previous_delta,omitempty"`
	MomentumChange    *int64              `json:"momentum_change,omitempty"`
}

type RepositoryTrendPage struct {
	Items    []RepositoryTrendMetric `json:"items"`
	Total    int                     `json:"total"`
	Coverage ComparisonCoverage      `json:"coverage"`
	HasMore  bool                    `json:"has_more"`
}

type RepositoryDetail struct {
	Metric      RepositoryMetric    `json:"metric"`
	Analysis    *RepositoryAnalysis `json:"analysis,omitempty"`
	History     []DailySnapshot     `json:"history"`
	FailedDates []Date              `json:"failed_dates"`
	ValidFrom   *Date               `json:"valid_from,omitempty"`
}

type TopicMetric struct {
	Topic               Topic        `json:"topic"`
	RepositoryCount     int          `json:"repository_count"`
	Growth              GrowthMetric `json:"growth"`
	ComparableDay       int          `json:"comparable_day"`
	ComparableSevenDay  int          `json:"comparable_seven_day"`
	ComparableThirtyDay int          `json:"comparable_thirty_day"`
	Concentration       *float64     `json:"concentration,omitempty"`
	LeaderRepositoryID  *int64       `json:"leader_repository_id,omitempty"`
	LeaderFullName      string       `json:"leader_full_name,omitempty"`
}

type TopicClassificationCoverage struct {
	RepositoryCount   int `json:"repository_count"`
	ClassifiedCount   int `json:"classified_count"`
	UnclassifiedCount int `json:"unclassified_count"`
}

type TopicHistoryPoint struct {
	Date          Date   `json:"date"`
	StarCount     *int64 `json:"star_count"`
	ObservedCount int    `json:"observed_count"`
	TargetCount   int    `json:"target_count"`
}

// GrowthHistoryPoint is one calendar day's aggregate growth across repositories
// with comparable successful observations recorded on that date. Later status
// changes never rewrite earlier points. Delta is nil when no repository has
// both a successful observation on Date and an earlier successful observation;
// a measured aggregate of zero remains a non-nil zero.
type GrowthHistoryPoint struct {
	Date                       Date   `json:"date"`
	Delta                      *int64 `json:"delta"`
	ComparableRepositoryCount  int    `json:"comparable_repository_count"`
	GapSpanningRepositoryCount int    `json:"gap_spanning_repository_count"`
}

type TopicDetail struct {
	Metric                 TopicMetric         `json:"metric"`
	Repositories           []RepositoryMetric  `json:"repositories"`
	History                []TopicHistoryPoint `json:"history"`
	Fastest                []RepositoryMetric  `json:"fastest"`
	ExcludedLeader         bool                `json:"excluded_leader"`
	ExcludedRepositoryID   *int64              `json:"excluded_repository_id,omitempty"`
	ExcludedRepositoryName string              `json:"excluded_repository_name,omitempty"`
}

type DashboardSummary struct {
	RepositoryCount int                  `json:"repository_count"`
	ActiveCount     int                  `json:"active_count"`
	TopicCount      int                  `json:"topic_count"`
	Coverage        CoverageMetric       `json:"coverage"`
	Growth          GrowthMetric         `json:"growth"`
	GrowthHistory   []GrowthHistoryPoint `json:"growth_history"`
	Fastest         []RepositoryMetric   `json:"fastest"`
	RecentRuns      []JobRun             `json:"recent_runs"`
}

type DiscoverySourceCount struct {
	Source          DiscoverySource `json:"source"`
	RepositoryCount int             `json:"repository_count"`
}

type DiscoveryProfileCount struct {
	Profile         string `json:"profile"`
	RepositoryCount int    `json:"repository_count"`
}

type DiscoverySummary struct {
	Sources          []DiscoverySourceCount  `json:"sources"`
	FirstSeenSources []DiscoverySourceCount  `json:"first_seen_sources"`
	Profiles         []DiscoveryProfileCount `json:"profiles"`
	Runs             []JobRun                `json:"runs"`
}
