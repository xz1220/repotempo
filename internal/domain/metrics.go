package domain

// GrowthMetric never interpolates an observation. Current is the most recent
// successful value on or before the query date. Each delta requires a real
// successful value on the query date and subtracts the latest successful value
// on or before its boundary (or, for Day, the immediately previous success).
// Missing baselines and failed current fetches therefore remain nil.
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
	Repository Repository   `json:"repository"`
	Growth     GrowthMetric `json:"growth"`
	Topics     []Topic      `json:"topics"`
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

type RepositoryDetail struct {
	Metric      RepositoryMetric `json:"metric"`
	History     []DailySnapshot  `json:"history"`
	FailedDates []Date           `json:"failed_dates"`
	ValidFrom   *Date            `json:"valid_from,omitempty"`
}

type TopicMetric struct {
	Topic              Topic        `json:"topic"`
	RepositoryCount    int          `json:"repository_count"`
	Growth             GrowthMetric `json:"growth"`
	Concentration      *float64     `json:"concentration,omitempty"`
	LeaderRepositoryID *int64       `json:"leader_repository_id,omitempty"`
	LeaderFullName     string       `json:"leader_full_name,omitempty"`
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
