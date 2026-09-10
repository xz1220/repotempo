package web

import (
	"slices"
	"strings"
)

// stripOwnerFields projects a page for anonymous readers when OAuth is enabled.
// This is not authorization: focus-scoped queries and management routes must be
// rejected before querying. Slice copies keep a shared Queryer result immutable.
func stripOwnerFields(data pageView) pageView {
	data.Dashboard.FastestRepositories = stripOwnerMetrics(data.Dashboard.FastestRepositories)
	data.Repositories.Items = stripOwnerMetrics(data.Repositories.Items)
	privateNote := data.Repository.Repository.ManualNote
	data.Repository.Repository = stripOwnerMetric(data.Repository.Repository)
	data.Repository.Analysis = stripCopiedOwnerAnalysis(data.Repository.Analysis, privateNote)
	data.Topic.Repositories = stripOwnerMetrics(data.Topic.Repositories)
	data.Radar.Fastest = stripOwnerRadar(data.Radar.Fastest)
	data.Radar.Slowest = stripOwnerRadar(data.Radar.Slowest)
	data.Radar.FallingBehind = stripOwnerRadar(data.Radar.FallingBehind)
	data.Radar.NewRepositories = stripOwnerRadar(data.Radar.NewRepositories)
	data.Boards = slices.Clone(data.Boards)
	for index := range data.Boards {
		data.Boards[index].Rows = slices.Clone(data.Boards[index].Rows)
		for row := range data.Boards[index].Rows {
			data.Boards[index].Rows[row].RepositoryMetric = stripOwnerMetric(data.Boards[index].Rows[row].RepositoryMetric)
		}
	}
	return data
}

func stripOwnerMetrics(values []RepositoryMetric) []RepositoryMetric {
	result := slices.Clone(values)
	for index := range result {
		result[index] = stripOwnerMetric(result[index])
	}
	return result
}

func stripOwnerRadar(values []RadarRepository) []RadarRepository {
	result := slices.Clone(values)
	for index := range result {
		result[index].RepositoryMetric = stripOwnerMetric(result[index].RepositoryMetric)
	}
	return result
}

func stripOwnerMetric(value RepositoryMetric) RepositoryMetric {
	value.Analysis = stripCopiedOwnerAnalysis(value.Analysis, value.ManualNote)
	value.IsFocus = false
	value.ManualNote = ""
	return value
}

func stripCopiedOwnerAnalysis(value *RepositoryAnalysis, privateNote string) *RepositoryAnalysis {
	if value == nil {
		return nil
	}
	// manual_note is also used for independently authored public project
	// descriptions. It is not a visibility flag. Hide only a direct copy of the
	// repository's private note, not all manual analyses or genuine AI research.
	note := normalizedOwnerText(privateNote)
	if note != "" && strings.EqualFold(strings.TrimSpace(value.Source), "manual_note") {
		fields := []string{value.SummaryZH, value.TechnicalNotes}
		fields = append(fields, value.KeyPoints...)
		fields = append(fields, value.UseCases...)
		for _, field := range fields {
			if normalizedOwnerText(field) == note {
				return nil
			}
		}
	}
	// Public templates only read these fields, but keep their objects and list
	// backing arrays separate too, so later presentation helpers remain safe.
	result := *value
	result.KeyPoints = slices.Clone(value.KeyPoints)
	result.UseCases = slices.Clone(value.UseCases)
	return &result
}

func normalizedOwnerText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
