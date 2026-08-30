package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	corestore "github.com/xz1220/github-radar/internal/store"
	"github.com/xz1220/github-radar/internal/web"
)

type WebAdapter struct {
	Store corestore.Store
}

var _ web.Queryer = WebAdapter{}

func (adapter WebAdapter) DashboardSummary(ctx context.Context, asOf time.Time) (web.DashboardSummary, error) {
	value, err := adapter.Store.DashboardSummary(ctx, domain.ShanghaiDate(asOf))
	if err != nil {
		return web.DashboardSummary{}, mapWebError(err)
	}
	coverage := mapCoverage(value.Coverage)
	return web.DashboardSummary{
		RepositoryTotal:       value.RepositoryCount,
		ActiveRepositoryTotal: value.ActiveCount,
		TopicTotal:            value.TopicCount,
		Coverage:              coverage,
		NewStars1D:            value.Growth.Day,
		NewStars7D:            value.Growth.SevenDay,
		NewStars30D:           value.Growth.ThirtyDay,
		GrowthHistory:         mapGrowthHistory(value.GrowthHistory),
		FastestRepositories:   mapRepositoryMetrics(value.Fastest),
		RecentRuns:            mapJobRuns(value.RecentRuns),
	}, nil
}

func (adapter WebAdapter) ListRepositoryMetrics(ctx context.Context, query web.RepositoryQuery) (web.RepositoryPage, error) {
	domainQuery := domain.RepositoryMetricQuery{
		AsOf:             domain.ShanghaiDate(query.AsOf),
		Search:           query.Search,
		TopicSlug:        query.TopicSlug,
		DiscoverySource:  validDiscoverySource(query.Source),
		MonitoringStatus: validMonitoringStatus(query.MonitoringStatus),
		Sort:             domain.RepositorySortThirtyDay,
		Descending:       true,
	}
	values, err := adapter.Store.ListRepositoryMetrics(ctx, domainQuery)
	if err != nil {
		return web.RepositoryPage{}, mapWebError(err)
	}
	total := len(values)
	values = pageRepositoryMetrics(values, query.Offset, query.Limit)
	topics, err := adapter.Store.ListTopics(ctx, domain.TopicActive)
	if err != nil {
		return web.RepositoryPage{}, mapWebError(err)
	}
	return web.RepositoryPage{
		Items:              mapRepositoryMetrics(values),
		Total:              total,
		Filter:             query,
		Topics:             mapTopicRefs(topics),
		Sources:            []string{"ossinsight", "github_search", "legacy", "manual"},
		MonitoringStatuses: []string{"active", "paused", "stopped"},
	}, nil
}

func (adapter WebAdapter) GetRepositoryDetail(ctx context.Context, id int64, asOf time.Time) (web.RepositoryDetail, error) {
	value, err := adapter.Store.GetRepositoryDetail(ctx, id, domain.ShanghaiDate(asOf))
	if err != nil {
		return web.RepositoryDetail{}, mapWebError(err)
	}
	history := make([]web.SnapshotPoint, 0, len(value.History))
	failed := make([]web.SnapshotPoint, 0, len(value.FailedDates))
	for _, point := range value.History {
		mapped := mapSnapshot(point)
		history = append(history, mapped)
		if point.FetchStatus == domain.FetchFailed {
			failed = append(failed, mapped)
		}
	}
	validFrom := datePointer(value.ValidFrom)
	return web.RepositoryDetail{
		Repository:    mapRepositoryMetric(value.Metric),
		History:       history,
		FailedDates:   failed,
		PreviousNames: append([]string(nil), value.Metric.Repository.PreviousNames...),
		ValidFrom:     validFrom,
	}, nil
}

func (adapter WebAdapter) ListTopicMetrics(ctx context.Context, asOf time.Time) (web.TopicPage, error) {
	values, err := adapter.Store.ListTopicMetrics(ctx, domain.ShanghaiDate(asOf))
	if err != nil {
		return web.TopicPage{}, mapWebError(err)
	}
	parentNames := adapter.parentTopicNames(ctx)
	items := make([]web.TopicMetric, 0, len(values))
	for _, value := range values {
		items = append(items, mapTopicMetric(value, parentNames))
	}
	return web.TopicPage{Items: items}, nil
}

func (adapter WebAdapter) GetTopicDetail(ctx context.Context, slug string, asOf time.Time, excludeLeader bool) (web.TopicDetail, error) {
	value, err := adapter.Store.GetTopicDetail(ctx, slug, domain.ShanghaiDate(asOf), excludeLeader)
	if err != nil {
		return web.TopicDetail{}, mapWebError(err)
	}
	parentNames := adapter.parentTopicNames(ctx)
	history := make([]web.TrendPoint, 0, len(value.History))
	for _, point := range value.History {
		stars := point.StarCount
		if point.TargetCount > 0 && point.ObservedCount < point.TargetCount {
			stars = nil
		}
		history = append(history, web.TrendPoint{Date: dateTime(point.Date), Stars: stars})
	}
	var concentrationPercent *float64
	if value.Metric.Concentration != nil {
		percent := *value.Metric.Concentration * 100
		concentrationPercent = &percent
	}
	return web.TopicDetail{
		Topic:                  mapTopicMetric(value.Metric, parentNames),
		Repositories:           mapRepositoryMetrics(value.Repositories),
		History:                history,
		ConcentrationPercent:   concentrationPercent,
		ExcludeLeader:          value.ExcludedLeader,
		ExcludedLeaderFullName: value.ExcludedRepositoryName,
	}, nil
}

func (adapter WebAdapter) DiscoverySummary(ctx context.Context) (web.DiscoverySummary, error) {
	value, err := adapter.Store.DiscoverySummary(ctx)
	if err != nil {
		return web.DiscoverySummary{}, mapWebError(err)
	}
	sources := mapDiscoverySources(value.Sources)
	firstSeenSources := mapDiscoverySources(value.FirstSeenSources)
	profiles := make([]web.DiscoveryProfile, 0, len(value.Profiles))
	for _, profile := range value.Profiles {
		profiles = append(profiles, web.DiscoveryProfile{Name: profile.Profile, NewRepositories: profile.RepositoryCount, CandidateCount: profile.RepositoryCount})
	}
	applyDiscoveryRunDetails(profiles, value.Runs)
	return web.DiscoverySummary{Sources: sources, FirstSeenSources: firstSeenSources, Profiles: profiles}, nil
}

func (adapter WebAdapter) ListJobRuns(ctx context.Context, limit, offset int) (web.RunsPage, error) {
	all, err := adapter.Store.ListJobRuns(ctx, 0, 0)
	if err != nil {
		return web.RunsPage{}, mapWebError(err)
	}
	total := len(all)
	page := pageJobRuns(all, offset, limit)
	coverageSummary, summaryErr := adapter.Store.DashboardSummary(ctx, domain.ShanghaiDate(time.Now()))
	result := web.RunsPage{Items: mapJobRuns(page), Total: total, Limit: limit, Offset: offset}
	if summaryErr == nil {
		result.CurrentCoverage = mapCoverage(coverageSummary.Coverage)
		if coverageSummary.Coverage.Date != "" && coverageSummary.Coverage.SuccessCount > 0 {
			date := dateTime(coverageSummary.Coverage.Date)
			result.LastSuccessfulSnapshotDate = &date
		}
	} else {
		result.Warnings = append(result.Warnings, "Current snapshot coverage is temporarily unavailable.")
	}
	return result, nil
}

func (adapter WebAdapter) Ready(ctx context.Context) error {
	_, err := adapter.Store.ListRepositories(ctx, domain.RepositoryFilter{Limit: 1})
	return err
}

func mapRepositoryMetrics(values []domain.RepositoryMetric) []web.RepositoryMetric {
	result := make([]web.RepositoryMetric, 0, len(values))
	for _, value := range values {
		result = append(result, mapRepositoryMetric(value))
	}
	return result
}

func mapRepositoryMetric(value domain.RepositoryMetric) web.RepositoryMetric {
	repository := value.Repository
	return web.RepositoryMetric{
		ID:               repository.GitHubRepoID,
		FullName:         repository.FullName,
		HTMLURL:          repository.HTMLURL,
		Description:      repository.Description,
		PrimaryLanguage:  repository.PrimaryLanguage,
		CurrentStars:     value.Growth.Current,
		Delta1D:          value.Growth.Day,
		Delta7D:          value.Growth.SevenDay,
		Delta30D:         value.Growth.ThirtyDay,
		Topics:           mapTopicRefs(value.Topics),
		FirstSeenSource:  string(repository.FirstSeenSource),
		FirstSeenProfile: repository.FirstSeenProfile,
		FirstSeenAt:      repository.FirstSeenAt,
		MonitoringStatus: string(repository.MonitoringStatus),
		GitHubStatus:     string(repository.GitHubStatus),
	}
}

func mapTopicRefs(values []domain.Topic) []web.TopicRef {
	result := make([]web.TopicRef, 0, len(values))
	for _, value := range values {
		result = append(result, web.TopicRef{Slug: value.Slug, Name: value.Name})
	}
	return result
}

func mapTopicMetric(value domain.TopicMetric, parentNames map[int64]domain.Topic) web.TopicMetric {
	result := web.TopicMetric{
		ID:              value.Topic.ID,
		Slug:            value.Topic.Slug,
		Name:            value.Topic.Name,
		Description:     value.Topic.Description,
		RepositoryCount: value.RepositoryCount,
		CurrentStars:    value.Growth.Current,
		Delta1D:         value.Growth.Day,
		Delta7D:         value.Growth.SevenDay,
		Delta30D:        value.Growth.ThirtyDay,
	}
	if value.Topic.ParentID != nil {
		if parent, ok := parentNames[*value.Topic.ParentID]; ok {
			result.ParentSlug = parent.Slug
			result.ParentName = parent.Name
		}
	}
	return result
}

func (adapter WebAdapter) parentTopicNames(ctx context.Context) map[int64]domain.Topic {
	values, err := adapter.Store.ListTopics(ctx, domain.TopicActive)
	if err != nil {
		return map[int64]domain.Topic{}
	}
	result := make(map[int64]domain.Topic, len(values))
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}

func mapSnapshot(value domain.DailySnapshot) web.SnapshotPoint {
	point := web.SnapshotPoint{
		Date:        dateTime(value.SnapshotDate),
		CapturedAt:  &value.CapturedAt,
		Stars:       value.StarCount,
		FetchStatus: string(value.FetchStatus),
		HTTPStatus:  value.HTTPStatus,
		ErrorCode:   value.ErrorCode,
	}
	if value.OSSTodayRank != nil {
		rank := int64(*value.OSSTodayRank)
		point.OSSRank = &rank
	}
	return point
}

func mapCoverage(value domain.CoverageMetric) web.SnapshotCoverage {
	result := web.SnapshotCoverage{
		Date:       dateTime(value.Date),
		Target:     value.TargetCount,
		Successful: value.SuccessCount,
		Failed:     value.FailureCount,
	}
	if value.TargetCount > 0 {
		percent := value.Percent
		result.Percent = &percent
	}
	return result
}

func mapGrowthHistory(values []domain.GrowthHistoryPoint) []web.GrowthPoint {
	result := make([]web.GrowthPoint, 0, len(values))
	for _, value := range values {
		result = append(result, web.GrowthPoint{
			Date:                       dateTime(value.Date),
			Delta:                      value.Delta,
			ComparableRepositoryCount:  value.ComparableRepositoryCount,
			GapSpanningRepositoryCount: value.GapSpanningRepositoryCount,
		})
	}
	return result
}

func mapDiscoverySources(values []domain.DiscoverySourceCount) []web.DiscoverySource {
	result := make([]web.DiscoverySource, 0, len(values))
	for _, value := range values {
		result = append(result, web.DiscoverySource{
			Source:          string(value.Source),
			RepositoryCount: value.RepositoryCount,
		})
	}
	return result
}

func mapJobRuns(values []domain.JobRun) []web.JobRun {
	result := make([]web.JobRun, 0, len(values))
	for _, value := range values {
		mapped := web.JobRun{
			RunID:        value.RunID,
			JobType:      value.JobType,
			StartedAt:    value.StartedAt,
			FinishedAt:   value.FinishedAt,
			Status:       string(value.Status),
			TargetCount:  value.TargetCount,
			SuccessCount: value.SuccessCount,
			FailureCount: value.FailureCount,
			SkippedCount: value.SkippedCount,
		}
		if value.ErrorSummary != "" {
			mapped.ErrorSummary = "One or more operations failed. Review the failure targets or server logs."
		}
		applyJobDetails(&mapped, value.Details)
		result = append(result, mapped)
	}
	return result
}

func applyJobDetails(target *web.JobRun, raw json.RawMessage) {
	var details struct {
		FailureRepositories []string `json:"failure_repositories"`
		SearchRateRemaining *int     `json:"search_rate_remaining"`
		CoreRateRemaining   *int     `json:"core_rate_remaining"`
		IncompleteResults   bool     `json:"incomplete_results"`
		QuerySplitCount     int      `json:"query_split_count"`
		Failures            []struct {
			Target   string `json:"target"`
			FullName string `json:"full_name"`
		} `json:"failures"`
		Snapshot struct {
			Failures []struct {
				FullName string `json:"full_name"`
			} `json:"failures"`
		} `json:"snapshot"`
	}
	if json.Unmarshal(raw, &details) != nil {
		return
	}
	target.FailureRepositories = append([]string(nil), details.FailureRepositories...)
	for _, failure := range details.Failures {
		name := failure.FullName
		if name == "" {
			name = failure.Target
		}
		if validRepositoryName(name) {
			target.FailureRepositories = append(target.FailureRepositories, name)
		}
	}
	for _, failure := range details.Snapshot.Failures {
		if validRepositoryName(failure.FullName) {
			target.FailureRepositories = append(target.FailureRepositories, failure.FullName)
		}
	}
	target.SearchRateRemaining = details.SearchRateRemaining
	target.CoreRateRemaining = details.CoreRateRemaining
	target.SearchIncomplete = details.IncompleteResults
	target.SearchSplitCount = details.QuerySplitCount
}

func validRepositoryName(value string) bool {
	if strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	parts := strings.Split(value, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" &&
		!strings.Contains(parts[0], ".") && !strings.Contains(parts[1], "/")
}

func applyDiscoveryRunDetails(profiles []web.DiscoveryProfile, runs []domain.JobRun) {
	byName := make(map[string]*web.DiscoveryProfile, len(profiles))
	for index := range profiles {
		byName[profiles[index].Name] = &profiles[index]
	}
	for _, run := range runs {
		var details struct {
			Profile           string `json:"profile"`
			CandidateCount    int    `json:"candidate_count"`
			CreatedCount      int    `json:"created_count"`
			IncompleteResults bool   `json:"incomplete_results"`
			QuerySplitCount   int    `json:"query_split_count"`
			Profiles          []struct {
				Name              string `json:"name"`
				HitCount          int    `json:"hit_count"`
				IncompleteResults bool   `json:"incomplete_results"`
				Truncated         bool   `json:"truncated"`
				SplitCount        int    `json:"split_count"`
			} `json:"profiles"`
		}
		if json.Unmarshal(run.Details, &details) != nil {
			continue
		}
		updates := details.Profiles
		if details.Profile != "" {
			updates = append(updates, struct {
				Name              string `json:"name"`
				HitCount          int    `json:"hit_count"`
				IncompleteResults bool   `json:"incomplete_results"`
				Truncated         bool   `json:"truncated"`
				SplitCount        int    `json:"split_count"`
			}{
				Name: details.Profile, HitCount: details.CandidateCount,
				IncompleteResults: details.IncompleteResults, SplitCount: details.QuerySplitCount,
			})
		}
		for _, update := range updates {
			profile, ok := byName[update.Name]
			if !ok || (profile.LastRunAt != nil && !run.StartedAt.After(*profile.LastRunAt)) {
				continue
			}
			when := run.StartedAt
			profile.LastRunAt = &when
			profile.CandidateCount = update.HitCount
			if details.Profile == update.Name && details.CreatedCount > 0 {
				profile.NewRepositories = details.CreatedCount
			}
			profile.IncompleteResults = update.IncompleteResults || update.Truncated
			profile.QuerySplitCount = update.SplitCount
		}
	}
	sort.SliceStable(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
}

func pageRepositoryMetrics(values []domain.RepositoryMetric, offset, limit int) []domain.RepositoryMetric {
	if offset >= len(values) {
		return []domain.RepositoryMetric{}
	}
	if offset < 0 {
		offset = 0
	}
	end := len(values)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return values[offset:end]
}

func pageJobRuns(values []domain.JobRun, offset, limit int) []domain.JobRun {
	if offset >= len(values) {
		return []domain.JobRun{}
	}
	if offset < 0 {
		offset = 0
	}
	end := len(values)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return values[offset:end]
}

func validDiscoverySource(value string) domain.DiscoverySource {
	source := domain.DiscoverySource(value)
	if source.Valid() {
		return source
	}
	return ""
}

func validMonitoringStatus(value string) domain.MonitoringStatus {
	status := domain.MonitoringStatus(value)
	if status.Valid() {
		return status
	}
	return ""
}

func dateTime(value domain.Date) time.Time {
	parsed, err := value.Time()
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func datePointer(value *domain.Date) *time.Time {
	if value == nil {
		return nil
	}
	parsed := dateTime(*value)
	return &parsed
}

func mapWebError(err error) error {
	if errors.Is(err, corestore.ErrNotFound) {
		return web.ErrNotFound
	}
	return err
}
