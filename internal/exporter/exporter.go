package exporter

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

type Source interface {
	corestore.RepositoryStore
	corestore.SnapshotStore
	corestore.TopicStore
	corestore.JobRunStore
}

type Dataset struct {
	GeneratedAt      time.Time                `json:"generated_at"`
	Repositories     []domain.Repository      `json:"repositories"`
	DailySnapshots   []domain.DailySnapshot   `json:"daily_snapshots"`
	Topics           []domain.Topic           `json:"topics"`
	RepositoryTopics []domain.RepositoryTopic `json:"repository_topics"`
	JobRuns          []domain.JobRun          `json:"job_runs"`
}

type Result struct {
	Format          string `json:"format"`
	Path            string `json:"path"`
	RepositoryCount int    `json:"repository_count"`
	SnapshotCount   int    `json:"snapshot_count"`
	TopicCount      int    `json:"topic_count"`
	AssignmentCount int    `json:"assignment_count"`
	JobRunCount     int    `json:"job_run_count"`
}

type Exporter struct {
	Source Source
	Backup corestore.Backuper
	Now    func() time.Time
}

func (exporter Exporter) Export(ctx context.Context, format, directory string) (Result, error) {
	if exporter.Source == nil {
		return Result{}, fmt.Errorf("exporter requires a data source")
	}
	if exporter.Now == nil {
		exporter.Now = time.Now
	}
	generatedAt := exporter.Now().UTC()
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return Result{}, fmt.Errorf("create export directory: %w", err)
	}
	stamp := generatedAt.Format("20060102T150405Z")
	if format == "sqlite" {
		if exporter.Backup == nil {
			return Result{}, fmt.Errorf("SQLite export is unavailable for this store")
		}
		path := filepath.Join(directory, "github-radar-"+stamp+".db")
		if err := exporter.Backup.Backup(ctx, path); err != nil {
			return Result{}, err
		}
		return Result{Format: format, Path: path}, nil
	}

	dataset, err := exporter.Load(ctx, generatedAt)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Format:          format,
		RepositoryCount: len(dataset.Repositories),
		SnapshotCount:   len(dataset.DailySnapshots),
		TopicCount:      len(dataset.Topics),
		AssignmentCount: len(dataset.RepositoryTopics),
		JobRunCount:     len(dataset.JobRuns),
	}
	switch format {
	case "json":
		result.Path = filepath.Join(directory, "github-radar-"+stamp+".json")
		err = writeJSON(result.Path, dataset)
	case "csv":
		result.Path = filepath.Join(directory, "github-radar-"+stamp+"-csv")
		err = writeCSVDirectory(result.Path, dataset)
	default:
		return Result{}, fmt.Errorf("unsupported export format %q; expected csv, json, or sqlite", format)
	}
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func (exporter Exporter) Load(ctx context.Context, generatedAt time.Time) (Dataset, error) {
	repositories, err := exporter.Source.ListRepositories(ctx, domain.RepositoryFilter{})
	if err != nil {
		return Dataset{}, fmt.Errorf("export repositories: %w", err)
	}
	dataset := Dataset{
		GeneratedAt:      generatedAt.UTC(),
		Repositories:     repositories,
		DailySnapshots:   []domain.DailySnapshot{},
		Topics:           []domain.Topic{},
		RepositoryTopics: []domain.RepositoryTopic{},
		JobRuns:          []domain.JobRun{},
	}
	for _, repository := range repositories {
		snapshots, err := exporter.Source.ListDailySnapshots(ctx, repository.GitHubRepoID, domain.SnapshotFilter{})
		if err != nil {
			return Dataset{}, fmt.Errorf("export snapshots for %s: %w", repository.FullName, err)
		}
		dataset.DailySnapshots = append(dataset.DailySnapshots, snapshots...)
	}
	activeTopics, err := exporter.Source.ListTopics(ctx, domain.TopicActive)
	if err != nil {
		return Dataset{}, fmt.Errorf("export active topics: %w", err)
	}
	archivedTopics, err := exporter.Source.ListTopics(ctx, domain.TopicArchived)
	if err != nil {
		return Dataset{}, fmt.Errorf("export archived topics: %w", err)
	}
	dataset.Topics = append(activeTopics, archivedTopics...)
	dataset.RepositoryTopics, err = exporter.Source.ListRepositoryTopicAssignments(ctx, nil)
	if err != nil {
		return Dataset{}, fmt.Errorf("export repository topic assignments: %w", err)
	}
	dataset.JobRuns, err = exporter.Source.ListJobRuns(ctx, 0, 0)
	if err != nil {
		return Dataset{}, fmt.Errorf("export job runs: %w", err)
	}
	return dataset, nil
}

func writeJSON(path string, dataset Dataset) error {
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("create JSON export: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(dataset)
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("write JSON export: %w", encodeErr)
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("close JSON export: %w", closeErr)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish JSON export: %w", err)
	}
	return nil
}

func writeCSVDirectory(path string, dataset Dataset) error {
	parent := filepath.Dir(path)
	temporary, err := os.MkdirTemp(parent, ".github-radar-csv-")
	if err != nil {
		return fmt.Errorf("create CSV export directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(temporary)
		}
	}()

	writers := []struct {
		name    string
		header  []string
		records [][]string
	}{
		{name: "repositories.csv", header: repositoryHeader(), records: repositoryRecords(dataset.Repositories)},
		{name: "daily_snapshots.csv", header: snapshotHeader(), records: snapshotRecords(dataset.DailySnapshots)},
		{name: "topics.csv", header: topicHeader(), records: topicRecords(dataset.Topics)},
		{name: "repository_topics.csv", header: assignmentHeader(), records: assignmentRecords(dataset.RepositoryTopics)},
		{name: "job_runs.csv", header: jobRunHeader(), records: jobRunRecords(dataset.JobRuns)},
	}
	for _, writer := range writers {
		if err := writeCSV(filepath.Join(temporary, writer.name), writer.header, writer.records); err != nil {
			return err
		}
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("publish CSV export directory: %w", err)
	}
	cleanup = false
	return nil
}

func writeCSV(path string, header []string, records [][]string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("create CSV export %s: %w", filepath.Base(path), err)
	}
	writer := csv.NewWriter(file)
	err = writer.Write(header)
	for _, record := range records {
		if err != nil {
			break
		}
		err = writer.Write(safeCSVRecord(record))
	}
	writer.Flush()
	if err == nil {
		err = writer.Error()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("write CSV export %s: %w", filepath.Base(path), err)
	}
	if closeErr != nil {
		return fmt.Errorf("close CSV export %s: %w", filepath.Base(path), closeErr)
	}
	return nil
}

func safeCSVRecord(record []string) []string {
	result := append([]string(nil), record...)
	for index, value := range result {
		if value == "" {
			continue
		}
		switch value[0] {
		case '=', '+', '-', '@', '\t', '\r':
			result[index] = "'" + value
		}
	}
	return result
}

func repositoryHeader() []string {
	return []string{"github_repo_id", "github_node_id", "full_name", "html_url", "description", "primary_language", "github_created_at", "first_seen_at", "first_seen_source", "first_seen_profile", "discovery_sources_json", "last_discovered_at", "monitoring_status", "github_status", "is_focus", "manual_note", "github_etag", "last_checked_at", "previous_names_json", "created_at", "updated_at"}
}

func repositoryRecords(values []domain.Repository) [][]string {
	records := make([][]string, 0, len(values))
	for _, value := range values {
		sources, _ := json.Marshal(value.DiscoverySources)
		previousNames, _ := json.Marshal(value.PreviousNames)
		records = append(records, []string{
			strconv.FormatInt(value.GitHubRepoID, 10), value.GitHubNodeID, value.FullName,
			value.HTMLURL, value.Description, value.PrimaryLanguage, timeValue(value.GitHubCreatedAt),
			value.FirstSeenAt.UTC().Format(time.RFC3339Nano), string(value.FirstSeenSource), value.FirstSeenProfile,
			string(sources), value.LastDiscoveredAt.UTC().Format(time.RFC3339Nano), string(value.MonitoringStatus),
			string(value.GitHubStatus), strconv.FormatBool(value.IsFocus), value.ManualNote, value.GitHubETag,
			timeValue(value.LastCheckedAt), string(previousNames), value.CreatedAt.UTC().Format(time.RFC3339Nano),
			value.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return records
}

func snapshotHeader() []string {
	return []string{"repository_id", "snapshot_date", "captured_at", "star_count", "fetch_status", "http_status", "error_code", "oss_today_rank", "oss_window_stars", "oss_total_score", "created_at"}
}

func snapshotRecords(values []domain.DailySnapshot) [][]string {
	records := make([][]string, 0, len(values))
	for _, value := range values {
		records = append(records, []string{
			strconv.FormatInt(value.RepositoryID, 10), value.SnapshotDate.String(), value.CapturedAt.UTC().Format(time.RFC3339Nano),
			int64Value(value.StarCount), string(value.FetchStatus), intValue(value.HTTPStatus), value.ErrorCode,
			intValue(value.OSSTodayRank), int64Value(value.OSSWindowStars), floatValue(value.OSSTotalScore),
			value.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return records
}

func topicHeader() []string {
	return []string{"id", "slug", "name", "parent_id", "description", "status", "created_at", "updated_at"}
}

func topicRecords(values []domain.Topic) [][]string {
	records := make([][]string, 0, len(values))
	for _, value := range values {
		records = append(records, []string{strconv.FormatInt(value.ID, 10), value.Slug, value.Name, int64Value(value.ParentID), value.Description, string(value.Status), value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano)})
	}
	return records
}

func assignmentHeader() []string {
	return []string{"repository_id", "topic_id", "source", "confirmed", "confidence", "assigned_at", "updated_at"}
}

func assignmentRecords(values []domain.RepositoryTopic) [][]string {
	records := make([][]string, 0, len(values))
	for _, value := range values {
		records = append(records, []string{strconv.FormatInt(value.RepositoryID, 10), strconv.FormatInt(value.TopicID, 10), string(value.Source), strconv.FormatBool(value.Confirmed), floatValue(value.Confidence), value.AssignedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano)})
	}
	return records
}

func jobRunHeader() []string {
	return []string{"run_id", "job_type", "started_at", "finished_at", "status", "target_count", "success_count", "failure_count", "skipped_count", "details_json", "error_summary", "created_at"}
}

func jobRunRecords(values []domain.JobRun) [][]string {
	records := make([][]string, 0, len(values))
	for _, value := range values {
		records = append(records, []string{value.RunID, value.JobType, value.StartedAt.UTC().Format(time.RFC3339Nano), timeValue(value.FinishedAt), string(value.Status), strconv.Itoa(value.TargetCount), strconv.Itoa(value.SuccessCount), strconv.Itoa(value.FailureCount), strconv.Itoa(value.SkippedCount), string(value.Details), value.ErrorSummary, value.CreatedAt.UTC().Format(time.RFC3339Nano)})
	}
	return records
}

func timeValue(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func intValue(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func int64Value(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func floatValue(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'g', -1, 64)
}
