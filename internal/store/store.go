package store

import (
	"context"
	"errors"

	"github.com/xz1220/github-radar/internal/domain"
)

var (
	ErrNotFound                   = errors.New("store: not found")
	ErrInvalid                    = errors.New("store: invalid value")
	ErrRepositoryIdentityMismatch = errors.New("store: repository identity mismatch")
)

type RepositoryStore interface {
	UpsertRepository(context.Context, domain.RepositoryObservation) (repository domain.Repository, created bool, err error)
	GetRepository(context.Context, int64) (domain.Repository, error)
	GetRepositoryByFullName(context.Context, string) (domain.Repository, error)
	ListRepositories(context.Context, domain.RepositoryFilter) ([]domain.Repository, error)
	SetRepositoryMonitoringStatus(context.Context, int64, domain.MonitoringStatus) error
	SetRepositoryGitHubStatus(context.Context, int64, domain.GitHubStatus) error
}

type SnapshotStore interface {
	PutDailySnapshot(context.Context, domain.DailySnapshot) (domain.SnapshotWriteResult, error)
	GetDailySnapshot(context.Context, int64, domain.Date) (domain.DailySnapshot, error)
	GetLatestSuccessfulSnapshot(context.Context, int64, domain.Date) (domain.DailySnapshot, error)
	ListDailySnapshots(context.Context, int64, domain.SnapshotFilter) ([]domain.DailySnapshot, error)
}

type TopicStore interface {
	UpsertTopic(context.Context, domain.Topic) (topic domain.Topic, created bool, err error)
	GetTopic(context.Context, int64) (domain.Topic, error)
	GetTopicBySlug(context.Context, string) (domain.Topic, error)
	ListTopics(context.Context, domain.TopicStatus) ([]domain.Topic, error)
	AssignRepositoryTopic(context.Context, domain.RepositoryTopic) (domain.TopicAssignmentResult, error)
	RemoveRepositoryTopic(context.Context, int64, int64, domain.TopicSource) (removed bool, err error)
	ListRepositoryTopics(context.Context, int64) ([]domain.Topic, error)
	ListRepositoryTopicAssignments(context.Context, *int64) ([]domain.RepositoryTopic, error)
}

type AnalysisStore interface {
	PutRepositoryAnalysis(context.Context, domain.RepositoryAnalysis) (domain.RepositoryAnalysis, error)
}

type JobRunStore interface {
	CreateJobRun(context.Context, domain.JobRun) error
	UpdateJobRun(context.Context, domain.JobRun) error
	GetJobRun(context.Context, string) (domain.JobRun, error)
	ListJobRuns(context.Context, int, int) ([]domain.JobRun, error)
}

type QueryStore interface {
	DashboardSummary(context.Context, domain.Date) (domain.DashboardSummary, error)
	ListRepositoryMetrics(context.Context, domain.RepositoryMetricQuery) ([]domain.RepositoryMetric, error)
	ListRepositoryTrends(context.Context, domain.RepositoryTrendQuery) (domain.RepositoryTrendPage, error)
	LatestSnapshotDate(context.Context) (domain.Date, error)
	GetRepositoryDetail(context.Context, int64, domain.Date) (domain.RepositoryDetail, error)
	ListTopicMetrics(context.Context, domain.Date) ([]domain.TopicMetric, error)
	TopicClassificationCoverage(context.Context, domain.Date) (domain.TopicClassificationCoverage, error)
	GetTopicDetail(context.Context, string, domain.Date, bool) (domain.TopicDetail, error)
	DiscoverySummary(context.Context) (domain.DiscoverySummary, error)
}

type Store interface {
	RepositoryStore
	SnapshotStore
	TopicStore
	AnalysisStore
	JobRunStore
	QueryStore
	Close() error
}

// Backuper is implemented by stores that can create a consistent native
// database export. It is intentionally separate from Store so future database
// implementations are not forced to expose SQLite-specific behavior.
type Backuper interface {
	Backup(context.Context, string) error
}
