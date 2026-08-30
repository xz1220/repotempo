package app

import (
	"context"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/exporter"
	"github.com/xz1220/github-radar/internal/service/discovery"
	"github.com/xz1220/github-radar/internal/service/snapshot"
	"github.com/xz1220/github-radar/internal/service/watch"
	"github.com/xz1220/github-radar/internal/source"
	"github.com/xz1220/github-radar/internal/source/legacydb"
)

const (
	ExitSuccess = 0
	ExitFailure = 1
	ExitUsage   = 2
	ExitPartial = 3
)

type DiscoverOptions struct {
	Source        string `json:"source"`
	Profile       string `json:"profile,omitempty"`
	LegacyPath    string `json:"legacy_path,omitempty"`
	DryRun        bool   `json:"dry_run"`
	DueOnly       bool   `json:"-"`
	IncludeLegacy bool   `json:"-"`
	IncludeManual bool   `json:"-"`
}

type OperationFailure struct {
	Stage   string `json:"stage"`
	Target  string `json:"target,omitempty"`
	Message string `json:"message"`
}

type SearchQueryReport struct {
	Query             string    `json:"query"`
	Depth             int       `json:"depth"`
	TotalCount        int       `json:"total_count"`
	IncompleteResults bool      `json:"incomplete_results"`
	Pages             int       `json:"pages"`
	Split             bool      `json:"split"`
	Truncated         bool      `json:"truncated"`
	RateRemaining     *int      `json:"rate_remaining,omitempty"`
	RateReset         time.Time `json:"rate_reset,omitempty"`
}

type SearchProfileReport struct {
	Name              string              `json:"name"`
	Due               bool                `json:"due"`
	Skipped           bool                `json:"skipped"`
	HitCount          int                 `json:"hit_count"`
	TotalCount        int                 `json:"total_count"`
	QueryCount        int                 `json:"query_count"`
	PageCount         int                 `json:"page_count"`
	SplitCount        int                 `json:"split_count"`
	IncompleteResults bool                `json:"incomplete_results"`
	Truncated         bool                `json:"truncated"`
	RateRemaining     *int                `json:"rate_remaining,omitempty"`
	RateReset         time.Time           `json:"rate_reset,omitempty"`
	QueryReports      []SearchQueryReport `json:"query_reports"`
	Error             string              `json:"error,omitempty"`
}

type OSSDiscoveryReport struct {
	RepositoryCount int            `json:"repository_count"`
	RowsByWindow    map[string]int `json:"rows_by_window"`
	WarningCount    int            `json:"warning_count"`
	Error           string         `json:"error,omitempty"`
}

type DiscoverReport struct {
	Source           string                         `json:"source"`
	DryRun           bool                           `json:"dry_run"`
	CandidateCount   int                            `json:"candidate_count"`
	CreatedCount     int                            `json:"created_count"`
	UpdatedCount     int                            `json:"updated_count"`
	SkippedCount     int                            `json:"skipped_count"`
	FailureCount     int                            `json:"failure_count"`
	TopicAssignments int                            `json:"topic_assignments"`
	Warnings         []source.Warning               `json:"warnings"`
	Failures         []OperationFailure             `json:"failures"`
	Profiles         []SearchProfileReport          `json:"profiles"`
	OSS              *OSSDiscoveryReport            `json:"ossinsight,omitempty"`
	Legacy           *legacydb.Stats                `json:"legacy,omitempty"`
	Registry         *discovery.RegistryReport      `json:"registry,omitempty"`
	OSSEvidence      map[int64]snapshot.OSSEvidence `json:"-"`
}

func (report DiscoverReport) Partial() bool {
	return report.FailureCount > 0 || len(report.Failures) > 0
}

type SnapshotCommandReport struct {
	snapshot.Report
	DryRun bool `json:"dry_run"`
}

func (report SnapshotCommandReport) Partial() bool {
	return report.FailureCount > 0 || report.MetadataFailureCount > 0
}

type ImportOptions struct {
	LegacyPath string   `json:"legacy_path,omitempty"`
	CSVPaths   []string `json:"csv_paths,omitempty"`
	DryRun     bool     `json:"dry_run"`
}

type ImportReport struct {
	DryRun                   bool                      `json:"dry_run"`
	CandidateCount           int                       `json:"candidate_count"`
	ObservationCount         int                       `json:"observation_count"`
	NonFixedObservationCount int                       `json:"non_fixed_observation_count"`
	ObservationSources       map[string]int            `json:"observation_sources"`
	InsertedCount            int                       `json:"inserted_count"`
	RepairedCount            int                       `json:"repaired_count"`
	ProtectedCount           int                       `json:"protected_count"`
	FailureCount             int                       `json:"failure_count"`
	Warnings                 []source.Warning          `json:"warnings"`
	Failures                 []OperationFailure        `json:"failures"`
	Legacy                   *legacydb.Stats           `json:"legacy,omitempty"`
	Registry                 *discovery.RegistryReport `json:"registry,omitempty"`
}

func (report ImportReport) Partial() bool { return report.FailureCount > 0 }

type ExportOptions struct {
	Format    string `json:"format"`
	Directory string `json:"directory"`
	DryRun    bool   `json:"dry_run"`
}

type ExportReport struct {
	exporter.Result
	DryRun      bool   `json:"dry_run"`
	PlannedPath string `json:"planned_path,omitempty"`
}

type DailyReport struct {
	DryRun    bool                  `json:"dry_run"`
	Fatal     bool                  `json:"fatal"`
	Runtime   DoctorReport          `json:"runtime"`
	Discovery DiscoverReport        `json:"discovery"`
	Snapshot  SnapshotCommandReport `json:"snapshot"`
	Exports   []exporter.Result     `json:"exports"`
	Cleaned   []string              `json:"cleaned"`
	Failures  []OperationFailure    `json:"failures"`
	RunID     string                `json:"run_id,omitempty"`
}

func (report DailyReport) Partial() bool {
	if report.Discovery.Partial() || report.Snapshot.Partial() || len(report.Failures) > 0 {
		return true
	}
	for _, check := range report.Runtime.Checks {
		if check.Status == CheckWarn {
			return true
		}
	}
	return false
}

// CommandApplication is the command-facing application boundary. Keeping
// parsing and formatting outside the concrete runtime makes every command
// testable without network access or a production database.
type CommandApplication interface {
	Discover(context.Context, DiscoverOptions) (DiscoverReport, error)
	Snapshot(context.Context, bool) (SnapshotCommandReport, error)
	ImportLegacy(context.Context, ImportOptions) (ImportReport, error)
	ListTopics(context.Context) ([]domain.Topic, error)
	AssignTopic(context.Context, string, string, bool) (domain.TopicAssignmentResult, error)
	RemoveTopic(context.Context, string, string, bool) (bool, error)
	WatchAdd(context.Context, string, string, bool, bool) (watch.AddResult, error)
	WatchSet(context.Context, string, domain.MonitoringStatus, bool) (domain.Repository, error)
	Export(context.Context, ExportOptions) (ExportReport, error)
	RunDaily(context.Context, bool) (DailyReport, error)
	Serve(context.Context, string) error
	Close() error
}
