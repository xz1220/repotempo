package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/xz1220/github-radar/internal/source"
)

type Options struct {
	Location      *time.Location
	DefaultSource string
}

type Result struct {
	Candidates   []source.Candidate
	Observations []source.Observation
	Warnings     []source.Warning
	RowsRead     int
}

func Import(reader io.Reader, options Options) (Result, error) {
	if reader == nil {
		return Result{}, errors.New("CSV reader is nil")
	}
	if options.Location == nil {
		options.Location = time.UTC
	}
	if options.DefaultSource == "" {
		options.DefaultSource = "legacy"
	}
	decoder := csv.NewReader(reader)
	decoder.ReuseRecord = false
	decoder.TrimLeadingSpace = true
	header, err := decoder.Read()
	if err != nil {
		return Result{}, fmt.Errorf("read CSV header: %w", err)
	}
	columns := make(map[string]int, len(header))
	for index, name := range header {
		normalized := normalizeHeader(name)
		if normalized == "" {
			continue
		}
		if _, duplicate := columns[normalized]; duplicate {
			return Result{}, fmt.Errorf("duplicate CSV header %q", name)
		}
		columns[normalized] = index
	}
	if !hasAny(columns, "github_repo_id", "repo_id", "repository_id") {
		return Result{}, errors.New("CSV requires github_repo_id (or repo_id/repository_id)")
	}
	if !hasAny(columns, "full_name", "repo_name", "repository") {
		return Result{}, errors.New("CSV requires full_name (or repo_name/repository)")
	}

	result := Result{}
	candidates := make([]source.Candidate, 0)
	observationKeys := make(map[string]struct{})
	for rowNumber := 2; ; rowNumber++ {
		row, readErr := decoder.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, fmt.Errorf("read CSV row %d: %w", rowNumber, readErr)
		}
		result.RowsRead++
		value := func(names ...string) string {
			for _, name := range names {
				if index, ok := columns[name]; ok && index < len(row) {
					return strings.TrimSpace(row[index])
				}
			}
			return ""
		}
		repoID, parseErr := strconv.ParseInt(value("github_repo_id", "repo_id", "repository_id"), 10, 64)
		fullName := value("full_name", "repo_name", "repository")
		if parseErr != nil || repoID <= 0 || !validFullName(fullName) {
			result.Warnings = append(result.Warnings, source.Warning{Row: rowNumber, Code: "invalid_repository", Message: "repository ID must be positive and full_name must be owner/name"})
			continue
		}
		originalSource := value("source", "first_seen_source")
		sourceName := normalizeSource(originalSource, options.DefaultSource)
		discoveredAt, discoveredErr := parseTime(value("first_seen_at", "discovered_at"), options.Location)
		if discoveredErr != nil {
			result.Warnings = append(result.Warnings, source.Warning{Row: rowNumber, Code: "invalid_first_seen_at", Message: discoveredErr.Error()})
		}
		createdAt, createdErr := parseTime(value("github_created_at"), options.Location)
		if createdErr != nil {
			result.Warnings = append(result.Warnings, source.Warning{Row: rowNumber, Code: "invalid_github_created_at", Message: createdErr.Error()})
		}
		focus, focusErr := parseOptionalBool(value("is_focus", "focus", "manual_favorite"))
		if focusErr != nil {
			result.Warnings = append(result.Warnings, source.Warning{Row: rowNumber, Code: "invalid_is_focus", Message: focusErr.Error()})
		}
		archived, _ := parseOptionalBool(value("archived"))
		fork, _ := parseOptionalBool(value("fork"))
		metadata := map[string]string{"csv_row": strconv.Itoa(rowNumber)}
		if originalSource != "" && originalSource != sourceName {
			metadata["original_source"] = originalSource
		}
		candidate := source.Candidate{
			Repository: source.Repository{
				ID:           repoID,
				NodeID:       value("github_node_id", "node_id"),
				FullName:     fullName,
				HTMLURL:      value("html_url"),
				Description:  value("description"),
				Language:     value("primary_language", "language"),
				Archived:     archived,
				Fork:         fork,
				GitHubStatus: value("github_status"),
				Topics:       splitList(value("topics", "github_topics")),
				CreatedAt:    createdAt,
			},
			Source:        sourceName,
			Profile:       value("profile", "first_seen_profile"),
			IsFocus:       focus,
			ManualNote:    value("manual_note", "note"),
			MonitorStatus: value("monitoring_status"),
			PreviousNames: splitList(value("previous_names", "old_names", "old_name")),
			Metadata:      metadata,
		}
		if discoveredAt != nil {
			candidate.DiscoveredAt = discoveredAt.UTC()
		}
		candidates = append(candidates, candidate)

		starsRaw := value("star_count", "github_stars", "stars")
		if starsRaw == "" {
			continue
		}
		stars, starsErr := strconv.ParseInt(starsRaw, 10, 64)
		if starsErr != nil || stars < 0 {
			result.Warnings = append(result.Warnings, source.Warning{Row: rowNumber, Code: "invalid_star_count", Message: starsRaw})
			continue
		}
		observedAt, observedErr := parseTime(value("observed_at", "captured_at"), options.Location)
		if observedErr != nil || observedAt == nil {
			result.Warnings = append(result.Warnings, source.Warning{Row: rowNumber, Code: "missing_observed_at", Message: "star observation omitted because actual observed_at/captured_at is unavailable"})
			continue
		}
		dateRaw := value("snapshot_date", "observation_date", "date")
		var snapshotDate time.Time
		if dateRaw == "" {
			local := observedAt.In(options.Location)
			snapshotDate = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, options.Location)
		} else {
			parsedDate, dateErr := time.ParseInLocation("2006-01-02", dateRaw, options.Location)
			if dateErr != nil {
				result.Warnings = append(result.Warnings, source.Warning{Row: rowNumber, Code: "invalid_snapshot_date", Message: dateErr.Error()})
				continue
			}
			snapshotDate = parsedDate
		}
		key := fmt.Sprintf("%d/%s", repoID, snapshotDate.Format("2006-01-02"))
		if _, duplicate := observationKeys[key]; duplicate {
			result.Warnings = append(result.Warnings, source.Warning{Row: rowNumber, Code: "duplicate_observation", Message: key})
			continue
		}
		observationKeys[key] = struct{}{}
		fixedPanel, fixedErr := parseOptionalBool(value("fixed_panel"))
		if fixedErr != nil {
			result.Warnings = append(result.Warnings, source.Warning{Row: rowNumber, Code: "invalid_fixed_panel", Message: fixedErr.Error()})
		}
		observationSource := originalSource
		if observationSource == "" {
			observationSource = sourceName
		}
		observationMetadata := map[string]string{"csv_row": strconv.Itoa(rowNumber)}
		for key, raw := range map[string]string{
			"oss_today_rank":   value("oss_today_rank"),
			"oss_window_stars": value("oss_window_stars"),
			"oss_total_score":  value("oss_total_score"),
		} {
			if raw != "" {
				observationMetadata[key] = raw
			}
		}
		result.Observations = append(result.Observations, source.Observation{
			RepositoryID: repoID,
			SnapshotDate: snapshotDate,
			ObservedAt:   observedAt.UTC(),
			StarCount:    stars,
			Source:       observationSource,
			FixedPanel:   fixedPanel,
			Metadata:     observationMetadata,
		})
	}
	result.Candidates = source.MergeCandidates(candidates)
	return result, nil
}

func normalizeHeader(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func normalizeSource(value, fallback string) string {
	if value == "" {
		value = fallback
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ossinsight":
		return "ossinsight"
	case "github_search", "github-search":
		return "github_search"
	case "manual":
		return "manual"
	case "legacy":
		return "legacy"
	default:
		return "legacy"
	}
}

func hasAny(columns map[string]int, names ...string) bool {
	for _, name := range names {
		if _, ok := columns[name]; ok {
			return true
		}
	}
	return false
}

func parseTime(value string, location *time.Location) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	formats := []string{time.RFC3339Nano, "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05", "2006-01-02"}
	for _, format := range formats {
		if parsed, err := time.ParseInLocation(format, value, location); err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("unsupported timestamp %q", value)
}

func parseOptionalBool(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "y":
		return true, nil
	case "0", "false", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("%q is not a boolean", value)
	}
}

func splitList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.Trim(value, "[]")
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '|' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), `"'`)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func validFullName(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
