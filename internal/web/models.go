package web

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound lets Queryer implementations distinguish an absent resource
// from an unavailable data source. Handlers turn it into the dashboard's 404
// page without exposing storage details.
var ErrNotFound = errors.New("web resource not found")

// Queryer is the read-only boundary used by the dashboard. Store packages can
// implement it directly or through a small adapter; HTTP handlers never issue
// SQL and never depend on a concrete database driver.
type Queryer interface {
	DashboardSummary(context.Context, time.Time) (DashboardSummary, error)
	ListRepositoryMetrics(context.Context, RepositoryQuery) (RepositoryPage, error)
	GetRepositoryDetail(context.Context, int64, time.Time) (RepositoryDetail, error)
	ListTopicMetrics(context.Context, time.Time) (TopicPage, error)
	GetTopicDetail(context.Context, string, time.Time, bool) (TopicDetail, error)
	DiscoverySummary(context.Context) (DiscoverySummary, error)
	ListJobRuns(context.Context, int, int) (RunsPage, error)
	Ready(context.Context) error
}

// DashboardSummary contains the first screen's decision-making metrics.
// Warnings describe incomplete secondary queries while the main result remains
// usable. Fatal query failures are returned as errors instead.
type DashboardSummary struct {
	Warnings              []string
	RepositoryTotal       int
	ActiveRepositoryTotal int
	TopicTotal            int
	Coverage              SnapshotCoverage
	NewStars1D            *int64
	NewStars7D            *int64
	NewStars30D           *int64
	FastestRepositories   []RepositoryMetric
	RecentRuns            []JobRun
}

type SnapshotCoverage struct {
	Date       time.Time
	Target     int
	Successful int
	Failed     int
	Percent    *float64
}

type RepositoryQuery struct {
	AsOf             time.Time
	Search           string
	TopicSlug        string
	Source           string
	MonitoringStatus string
	Limit            int
	Offset           int
}

type RepositoryPage struct {
	Warnings           []string
	Items              []RepositoryMetric
	Total              int
	Filter             RepositoryQuery
	Topics             []TopicRef
	Sources            []string
	MonitoringStatuses []string
}

type RepositoryMetric struct {
	ID               int64
	FullName         string
	HTMLURL          string
	Description      string
	PrimaryLanguage  string
	CurrentStars     *int64
	Delta1D          *int64
	Delta7D          *int64
	Delta30D         *int64
	Topics           []TopicRef
	FirstSeenSource  string
	FirstSeenProfile string
	FirstSeenAt      time.Time
	MonitoringStatus string
	GitHubStatus     string
}

type TopicRef struct {
	Slug string
	Name string
}

type RepositoryDetail struct {
	Warnings      []string
	Repository    RepositoryMetric
	History       []SnapshotPoint
	FailedDates   []SnapshotPoint
	PreviousNames []string
	ValidFrom     *time.Time
}

type SnapshotPoint struct {
	Date        time.Time
	CapturedAt  *time.Time
	Stars       *int64
	FetchStatus string
	HTTPStatus  *int
	ErrorCode   string
	OSSRank     *int64
}

type TopicPage struct {
	Warnings []string
	Items    []TopicMetric
}

type TopicMetric struct {
	ID              int64
	Slug            string
	Name            string
	ParentSlug      string
	ParentName      string
	Description     string
	RepositoryCount int
	CurrentStars    *int64
	Delta1D         *int64
	Delta7D         *int64
	Delta30D        *int64
}

type TopicDetail struct {
	Warnings               []string
	Topic                  TopicMetric
	Repositories           []RepositoryMetric
	History                []TrendPoint
	ConcentrationPercent   *float64
	ExcludeLeader          bool
	ExcludedLeaderFullName string
}

type TrendPoint struct {
	Date  time.Time
	Stars *int64
}

type DiscoverySummary struct {
	Warnings []string
	Sources  []DiscoverySource
	Profiles []DiscoveryProfile
}

type DiscoverySource struct {
	Source          string
	RepositoryCount int
	LastRunAt       *time.Time
}

type DiscoveryProfile struct {
	Name              string
	NewRepositories   int
	CandidateCount    int
	IncompleteResults bool
	QuerySplitCount   int
	LastRunAt         *time.Time
}

type RunsPage struct {
	Warnings                   []string
	Items                      []JobRun
	Total                      int
	Limit                      int
	Offset                     int
	LastSuccessfulSnapshotDate *time.Time
	CurrentCoverage            SnapshotCoverage
}

type JobRun struct {
	RunID               string
	JobType             string
	StartedAt           time.Time
	FinishedAt          *time.Time
	Status              string
	TargetCount         int
	SuccessCount        int
	FailureCount        int
	SkippedCount        int
	FailureRepositories []string
	ErrorSummary        string
	SearchRateRemaining *int
	CoreRateRemaining   *int
	SearchIncomplete    bool
	SearchSplitCount    int
}
