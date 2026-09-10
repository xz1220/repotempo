package web

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestStripOwnerFieldsCoversEveryRepositoryContainerWithoutMutatingQueryResults(t *testing.T) {
	const note = "只给管理员看的私人观察记录"
	analysis := &RepositoryAnalysis{SummaryZH: note, Source: "manual_note", KeyPoints: []string{"原始能力"}, UseCases: []string{"原始场景"}}
	metric := RepositoryMetric{ID: 101, FullName: "acme/radar", IsFocus: true, ManualNote: note, Analysis: analysis, CurrentStars: int64Pointer(900), Description: "Public description"}
	sharedMetrics := []RepositoryMetric{metric}
	sharedRadar := []RadarRepository{{RepositoryMetric: metric}}
	sharedRows := []boardRow{{RadarRepository: sharedRadar[0], Rank: 1, BarWidth: "100"}}
	input := pageView{
		Dashboard:    DashboardSummary{FastestRepositories: sharedMetrics},
		Repositories: RepositoryPage{Items: sharedMetrics, Total: 1, RegistryTotal: 1, FocusTotal: 1},
		Repository:   RepositoryDetail{Repository: metric, Analysis: analysis},
		Topic:        TopicDetail{Repositories: sharedMetrics},
		Radar:        RadarOverview{Fastest: sharedRadar, Slowest: sharedRadar, FallingBehind: sharedRadar, NewRepositories: sharedRadar},
		Boards:       []leaderboard{{Rows: sharedRows}, {Rows: sharedRows}},
	}
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	public := stripOwnerFields(input)
	metrics := []RepositoryMetric{
		public.Dashboard.FastestRepositories[0], public.Repositories.Items[0], public.Repository.Repository,
		public.Topic.Repositories[0], public.Radar.Fastest[0].RepositoryMetric, public.Radar.Slowest[0].RepositoryMetric,
		public.Radar.FallingBehind[0].RepositoryMetric, public.Radar.NewRepositories[0].RepositoryMetric,
		public.Boards[0].Rows[0].RepositoryMetric, public.Boards[1].Rows[0].RepositoryMetric,
	}
	for index, value := range metrics {
		if value.IsFocus || value.ManualNote != "" || value.Analysis != nil {
			t.Errorf("container %d exposes owner fields: %+v", index, value)
		}
		if value.ID != 101 || value.FullName != "acme/radar" || value.CurrentStars == nil || *value.CurrentStars != 900 || value.Description != "Public description" {
			t.Errorf("container %d lost public facts", index)
		}
	}
	if public.Repository.Analysis != nil {
		t.Fatal("detail's separate Analysis pointer leaked the copied note")
	}
	if public.Repositories.RegistryTotal != 1 || public.Repositories.FocusTotal != 0 {
		t.Fatalf("anonymous summary counts = registry %d, focus %d", public.Repositories.RegistryTotal, public.Repositories.FocusTotal)
	}
	after, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("anonymous projection mutated data used by later administrator requests")
	}
	// Check outer and inner slice isolation, including containers sharing the
	// same backing arrays before projection.
	public.Repositories.Items[0].FullName = "anonymous/list-only"
	public.Radar.Fastest[0].FullName = "anonymous/radar-only"
	public.Boards[0].Rows[0].FullName = "anonymous/board-only"
	if input.Repositories.Items[0].FullName != metric.FullName || input.Radar.Fastest[0].FullName != metric.FullName || input.Boards[0].Rows[0].FullName != metric.FullName || public.Boards[1].Rows[0].FullName != metric.FullName || public.Radar.Slowest[0].FullName != metric.FullName {
		t.Fatal("projection retained mutable owner/shared container backing arrays")
	}
}

func TestStripOwnerFieldsRetainsPublicManualAndAIAnalyses(t *testing.T) {
	for _, source := range []string{"manual_note", "manual", "legacy_research", "codex", "kimi-code"} {
		t.Run(source, func(t *testing.T) {
			analysis := &RepositoryAnalysis{SummaryZH: "公开的项目用途", Source: source, KeyPoints: []string{"公开能力"}, UseCases: []string{"公开场景"}, TechnicalNotes: "公开技术信息"}
			input := pageView{Repositories: RepositoryPage{Items: []RepositoryMetric{{ID: 101, IsFocus: true, ManualNote: "私人投资备注", Analysis: analysis}}}}
			public := stripOwnerFields(input)
			got := public.Repositories.Items[0].Analysis
			if got == nil || !reflect.DeepEqual(got, analysis) {
				t.Fatal("public project interpretation was hidden or changed")
			}
			if got == analysis {
				t.Fatal("analysis object was not isolated from the Queryer result")
			}
			got.SummaryZH, got.KeyPoints[0], got.UseCases[0] = "changed", "changed", "changed"
			if analysis.SummaryZH != "公开的项目用途" || analysis.KeyPoints[0] != "公开能力" || analysis.UseCases[0] != "公开场景" {
				t.Fatal("public analysis retained shared mutable storage")
			}
		})
	}
}

func TestStripOwnerFieldsOnlyHidesDirectPrivateNoteCopies(t *testing.T) {
	const note = "私人投资计划"
	for _, field := range []string{"summary", "technical", "key_point", "use_case"} {
		t.Run(field, func(t *testing.T) {
			analysis := &RepositoryAnalysis{Source: " MANUAL_NOTE ", SummaryZH: "公开项目用途"}
			switch field {
			case "summary":
				analysis.SummaryZH = note
			case "technical":
				analysis.TechnicalNotes = note
			case "key_point":
				analysis.KeyPoints = []string{note}
			case "use_case":
				analysis.UseCases = []string{note}
			}
			if got := stripCopiedOwnerAnalysis(analysis, " \n"+note+"\t"); got != nil {
				t.Fatal("private note was copied into a public manual analysis")
			}
		})
	}
	public := &RepositoryAnalysis{Source: "manual_note", SummaryZH: "公开说明里提到同一个投资主题，但不是私人记录"}
	if stripCopiedOwnerAnalysis(public, "投资") == nil {
		t.Fatal("substring matching wrongly hid an independently written public analysis")
	}
	if stripCopiedOwnerAnalysis(&RepositoryAnalysis{Source: "manual_note", SummaryZH: note}, "") == nil {
		t.Fatal("source alone is not proof that an analysis is private")
	}
	if stripCopiedOwnerAnalysis(&RepositoryAnalysis{Source: "codex", SummaryZH: note}, note) == nil {
		t.Fatal("the helper must not classify genuine public AI research by text coincidence")
	}
}

func TestStripOwnerFieldsNilAndIdempotence(t *testing.T) {
	empty := pageView{}
	if result := stripOwnerFields(empty); !reflect.DeepEqual(result, empty) {
		t.Fatal("nil page changed shape")
	}
	value := pageView{Repository: RepositoryDetail{Repository: RepositoryMetric{ID: 101, IsFocus: true, ManualNote: "owner", Analysis: &RepositoryAnalysis{Source: "codex", SummaryZH: "public"}}}}
	once := stripOwnerFields(value)
	if twice := stripOwnerFields(once); !reflect.DeepEqual(once, twice) {
		t.Fatal("redaction was not idempotent")
	}
}
