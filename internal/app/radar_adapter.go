package app

import (
	"context"
	"fmt"

	"github.com/xz1220/github-radar/internal/domain"
	corestore "github.com/xz1220/github-radar/internal/store"
	"github.com/xz1220/github-radar/internal/web"
)

var _ web.RadarQueryer = WebAdapter{}

func (adapter WebAdapter) RadarOverview(ctx context.Context, query web.RepositoryQuery) (web.RadarOverview, error) {
	asOf, err := adapter.resolveAsOf(ctx, query.AsOf)
	if err != nil {
		return web.RadarOverview{}, err
	}
	radarStore, ok := adapter.Store.(corestore.RadarStore)
	if !ok {
		return web.RadarOverview{}, fmt.Errorf("radar overview is unavailable for this store")
	}
	value, err := radarStore.RadarOverview(ctx, domain.RepositoryTrendQuery{
		AsOf: domain.ShanghaiDate(asOf), WindowDays: query.WindowDays,
		TopicSlug: query.TopicSlug, DiscoverySource: validDiscoverySource(query.Source),
		MonitoringStatus: validMonitoringStatus(query.MonitoringStatus),
		OnlyFocus:        query.OnlyFocus,
	})
	if err != nil {
		return web.RadarOverview{}, mapWebError(err)
	}
	query.AsOf, query.WindowDays = asOf, value.WindowDays
	topics, err := adapter.Store.ListTopics(ctx, domain.TopicActive)
	if err != nil {
		return web.RadarOverview{}, mapWebError(err)
	}
	coverage := value.Coverage
	result := web.RadarOverview{
		Filter: query, AsOf: asOf, BaselineDate: dateTime(value.BaselineDate), PreviousDate: dateTime(value.PreviousDate),
		Coverage: web.RadarCoverage{
			ComparisonCoverage: mapComparisonCoverage(coverage.ComparisonCoverage),
			FailedCount:        coverage.FailedCount, MissingCount: coverage.MissingCount, StaleCount: coverage.StaleCount,
			UpCount: coverage.UpCount, FlatCount: coverage.FlatCount, DownCount: coverage.DownCount,
			MomentumComparableCount: coverage.MomentumComparableCount, SlowingCount: coverage.SlowingCount,
		},
		Fastest: mapRadarRepositories(value.Fastest), Slowest: mapRadarRepositories(value.Slowest),
		FallingBehind: mapRadarRepositories(value.FallingBehind), NewRepositories: mapRadarRepositories(value.NewRepositories),
		History: make([]web.RadarHistoryPoint, 0, len(value.History)), Topics: mapTopicFilterRefs(topics),
	}
	for _, point := range value.History {
		result.History = append(result.History, web.RadarHistoryPoint{
			Date: dateTime(point.Date), Stars: point.Stars, Index: point.Index,
			ObservedCount: point.ObservedCount, CohortCount: point.CohortCount,
		})
	}
	return result, nil
}

func mapRadarRepositories(values []domain.RepositoryTrendMetric) []web.RadarRepository {
	metrics := mapRepositoryTrends(values)
	result := make([]web.RadarRepository, 0, len(values))
	for index, value := range values {
		entry := web.RadarRepository{
			RepositoryMetric: metrics[index], LastObservedStars: value.LastObservedStars, IsStale: value.IsStale,
			PreviousDelta: value.PreviousDelta, MomentumChange: value.MomentumChange,
			GitHubCreatedAt: value.Repository.GitHubCreatedAt,
		}
		if value.LastObservedDate != nil {
			observed := dateTime(*value.LastObservedDate)
			entry.LastObservedAt = &observed
		}
		result = append(result, entry)
	}
	return result
}
