package manual

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xz1220/github-radar/internal/source"
	"gopkg.in/yaml.v3"
)

type Resolver interface {
	ResolveRepository(context.Context, string, int64) (source.Repository, error)
}

type File struct {
	Version      int     `yaml:"version"`
	Repositories []Entry `yaml:"repositories"`
}

type Entry struct {
	FullName      string   `yaml:"full_name"`
	GitHubRepoID  int64    `yaml:"github_repo_id"`
	Focus         bool     `yaml:"focus"`
	Note          string   `yaml:"note"`
	Monitoring    string   `yaml:"monitoring_status"`
	Topics        []string `yaml:"topics"`
	PreviousNames []string `yaml:"previous_names"`
}

type Result struct {
	Candidates []source.Candidate
	Warnings   []source.Warning
	RowsRead   int
}

func Import(ctx context.Context, reader io.Reader, resolver Resolver, now time.Time) (Result, error) {
	if reader == nil {
		return Result{}, errors.New("manual input reader is nil")
	}
	if resolver == nil {
		return Result{}, errors.New("manual import requires a GitHub repository resolver")
	}
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var file File
	if err := decoder.Decode(&file); err != nil {
		return Result{}, fmt.Errorf("decode manual YAML/JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Result{}, errors.New("manual input must contain one document")
		}
		return Result{}, err
	}
	if file.Version != 1 {
		return Result{}, fmt.Errorf("unsupported manual input version %d", file.Version)
	}
	result := Result{RowsRead: len(file.Repositories)}
	candidates := make([]source.Candidate, 0, len(file.Repositories))
	for index, entry := range file.Repositories {
		row := index + 1
		entry.FullName = strings.TrimSpace(entry.FullName)
		if !validFullName(entry.FullName) {
			result.Warnings = append(result.Warnings, source.Warning{Row: row, Code: "invalid_full_name", Message: "manual full_name must be owner/name"})
			continue
		}
		if entry.GitHubRepoID < 0 {
			result.Warnings = append(result.Warnings, source.Warning{Row: row, Code: "invalid_repository_id", Message: "github_repo_id must be positive when provided"})
			continue
		}
		if entry.Monitoring == "" {
			entry.Monitoring = "active"
		}
		switch entry.Monitoring {
		case "active", "paused", "stopped":
		default:
			result.Warnings = append(result.Warnings, source.Warning{Row: row, Code: "invalid_monitoring_status", Message: entry.Monitoring})
			continue
		}
		repository, err := resolver.ResolveRepository(ctx, entry.FullName, entry.GitHubRepoID)
		if err != nil {
			result.Warnings = append(result.Warnings, source.Warning{Row: row, Code: "github_resolution_failed", Message: err.Error()})
			continue
		}
		if repository.ID <= 0 || repository.FullName == "" {
			result.Warnings = append(result.Warnings, source.Warning{Row: row, Code: "invalid_github_response", Message: "resolver omitted repository ID or full name"})
			continue
		}
		if entry.GitHubRepoID > 0 && repository.ID != entry.GitHubRepoID {
			result.Warnings = append(result.Warnings, source.Warning{Row: row, Code: "repository_id_mismatch", Message: fmt.Sprintf("expected %d, got %d", entry.GitHubRepoID, repository.ID)})
			continue
		}
		repository.Topics = appendUnique(repository.Topics, entry.Topics...)
		previousNames := append([]string(nil), entry.PreviousNames...)
		if repository.FullName != entry.FullName {
			previousNames = appendUnique(previousNames, entry.FullName)
		}
		candidates = append(candidates, source.Candidate{
			Repository:    repository,
			Source:        "manual",
			Profile:       "config-watchlist",
			DiscoveredAt:  now.UTC(),
			IsFocus:       entry.Focus,
			ManualNote:    entry.Note,
			MonitorStatus: entry.Monitoring,
			PreviousNames: previousNames,
		})
	}
	result.Candidates = source.MergeCandidates(candidates)
	return result, nil
}

func validFullName(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}
