package web

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound lets Queryer implementations distinguish an absent resource
// from an unavailable data source. Handlers turn it into the dashboard's 404
// page without exposing storage details.
var (
	ErrNotFound = errors.New("web resource not found")
	ErrInvalid  = errors.New("web request is invalid")
)

// Queryer is the read-only boundary used by the dashboard. Store packages can
// implement it directly or through a small adapter; HTTP handlers never issue
// SQL and never depend on a concrete database driver.
type Queryer interface {
	DashboardSummary(context.Context, time.Time) (DashboardSummary, error)
	ListRepositoryMetrics(context.Context, RepositoryQuery) (RepositoryPage, error)
	ListRepositoryTrends(context.Context, RepositoryQuery) (RepositoryPage, error)
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
	GrowthHistory         []GrowthPoint
	FastestRepositories   []RepositoryMetric
	RecentRuns            []JobRun
}

// GrowthPoint is one daily, registry-wide Star change. Delta is nil when the
// day has no trustworthy comparison; a non-nil zero is a real measured value.
type GrowthPoint struct {
	Date                       time.Time
	Delta                      *int64
	ComparableRepositoryCount  int
	GapSpanningRepositoryCount int
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
	WindowDays       int
	Search           string
	TopicSlug        string
	Tag              string
	Source           string
	MonitoringStatus string
	Sort             string
	OnlyNew          bool
	OnlyFocus        bool
	Limit            int
	Offset           int
	AfterID          *int64
}

type RepositoryPage struct {
	Warnings           []string
	Items              []RepositoryMetric
	Total              int
	Filter             RepositoryQuery
	Topics             []TopicRef
	Tags               []TagRef
	Sources            []string
	MonitoringStatuses []string
	Coverage           ComparisonCoverage
	HasMore            bool
	NextCursor         string
	Path               string
	FirstPageURL       string
}

type ComparisonCoverage struct {
	BaselineDate    time.Time
	AsOfDate        time.Time
	ScopeCount      int
	ObservedCount   int
	ComparableCount int
	NewCount        int
}

type RepositoryMetric struct {
	ID                int64
	FullName          string
	HTMLURL           string
	Description       string
	PrimaryLanguage   string
	CurrentStars      *int64
	Delta1D           *int64
	Delta7D           *int64
	Delta30D          *int64
	Topics            []TopicRef
	Tags              []string
	Analysis          *RepositoryAnalysis
	Activity          *RepositoryActivity
	FirstSeenSource   string
	DiscoverySources  []string
	FirstSeenProfile  string
	FirstSeenAt       time.Time
	MonitoringStatus  string
	GitHubStatus      string
	ManualNote        string
	BaselineStars     *int64
	CurrentRank       *int64
	BaselineRank      *int64
	RankChange        *int64
	StarDelta         *int64
	GrowthRate        *float64
	DailyVelocity     *float64
	IsNew             bool
	IsFocus           bool
	IsStale           bool
	LastObservedAt    *time.Time
	LastObservedStars *int64
	GitHubCreatedAt   *time.Time
	PreviousDelta     *int64
	MomentumChange    *int64
}

type TopicRef struct {
	Slug       string
	Name       string
	ParentSlug string
	ParentName string
	IsParent   bool
}

type TagRef struct {
	Name  string
	Count int
}

type RepositoryDetail struct {
	Warnings      []string
	AsOf          time.Time
	Repository    RepositoryMetric
	Analysis      *RepositoryAnalysis
	History       []SnapshotPoint
	FailedDates   []SnapshotPoint
	PreviousNames []string
	ValidFrom     *time.Time
}

type RepositoryAnalysis struct {
	SummaryZH      string
	KeyPoints      []string
	UseCases       []string
	TechnicalNotes string
	Source         string
	Model          string
	Revision       int
	AnalyzedAt     time.Time
}

// RepositoryActivity is a cached GitHub default-branch sample, not AI analysis
// or historical activity as of the currently selected Star observation date.
type RepositoryActivity struct {
	RepositoryID   int64
	DefaultBranch  string
	HeadSHA        string
	IsFork         bool
	WindowStart    time.Time
	WindowEnd      time.Time
	FetchedAt      time.Time
	LatestCommitAt *time.Time
	Commits        int
	ActiveDays     int
	Complete       bool
	Daily          []ActivityDay
}

type ActivityDay struct {
	Date  string
	Count int
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
	Warnings       []string
	AsOf           time.Time
	Items          []TopicMetric
	Classification TopicClassificationCoverage
}

type TopicClassificationCoverage struct {
	RepositoryCount   int
	ClassifiedCount   int
	UnclassifiedCount int
	Percent           *float64
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
	Comparable1D    int
	Comparable7D    int
	Comparable30D   int
}

type TopicDetail struct {
	Warnings               []string
	AsOf                   time.Time
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
	Warnings         []string
	Sources          []DiscoverySource
	FirstSeenSources []DiscoverySource
	Profiles         []DiscoveryProfile
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
	TrendingWindows     []TrendingWindowStatus
	TrendingSkipped     bool
	TrendingSkipReason  string
}

type TrendingWindowStatus struct {
	Period string
	Count  int
	Error  string
}
