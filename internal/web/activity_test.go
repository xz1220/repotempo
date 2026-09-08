package web

import (
	"html"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func activityFixture() *RepositoryActivity {
	end := time.Date(2026, 9, 8, 12, 30, 0, 0, time.UTC)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	value := &RepositoryActivity{RepositoryID: 101, DefaultBranch: "main", HeadSHA: strings.Repeat("a", 40), WindowStart: start, WindowEnd: end, FetchedAt: end.Add(time.Minute), LatestCommitAt: &end, Commits: 6, ActiveDays: 3, Complete: true}
	for index := 0; index < 30; index++ {
		count := 0
		if index >= 27 {
			count = index - 26
		}
		value.Daily = append(value.Daily, ActivityDay{Date: start.AddDate(0, 0, index).Format("2006-01-02"), Count: count})
	}
	return value
}

func TestRepositoryAgeUsesObservationCalendarNotContinuousDevelopment(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	asOf := time.Date(2026, 9, 8, 0, 0, 0, 0, location)
	createdToday := asOf.Add(20 * time.Hour)
	createdYesterday := asOf.Add(-time.Hour)
	future := asOf.Add(24 * time.Hour)
	old := asOf.AddDate(-4, 0, 0)
	for _, locale := range []string{localeChinese, localeEnglish} {
		l := newLocalizer(locale)
		for _, test := range []struct {
			name    string
			created *time.Time
			key     string
		}{
			{"unknown", nil, "activity.age_unknown"}, {"same calendar day", &createdToday, "activity.created_today"},
			{"yesterday", &createdYesterday, "activity.age_day"}, {"future", &future, "activity.not_created"},
		} {
			t.Run(locale+"/"+test.name, func(t *testing.T) {
				view := repositoryAge(test.created, asOf, location, l)
				if view.Label != l.Text(test.key) || !strings.Contains(view.Help, l.Text("activity.age_help")) {
					t.Fatalf("age incorrectly implies development time: %+v", view)
				}
			})
		}
		view := repositoryAge(&old, asOf, location, l)
		if !strings.Contains(view.Label, "4.0") || !strings.Contains(view.Help, "2026-09-08") {
			t.Fatalf("age was not calculated at observation date: %+v", view)
		}
	}
}

func TestActivityUnknownZeroAndIncompleteRemainDistinct(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		l := newLocalizer(locale)
		repository := RepositoryMetric{ID: 101, FullName: "acme/radar"}
		now := mustDate("2026-09-09")
		unknown := activityView(repository, mustDate("2026-08-30"), now, l)
		if unknown.HasCache || unknown.Summary != "" || len(unknown.Bars) != 0 || unknown.CommitsURL != "https://github.com/acme/radar/commits" {
			t.Fatalf("unknown must not claim zero: %+v", unknown)
		}
		value := activityFixture()
		value.Commits, value.ActiveDays, value.LatestCommitAt = 0, 0, nil
		for index := range value.Daily {
			value.Daily[index].Count = 0
		}
		repository.Activity = value
		zero := activityView(repository, mustDate("2026-08-30"), now, l)
		if !zero.HasCache || zero.Summary != l.Textf("activity.counts", 0, 0) || zero.LatestLabel != l.Text("activity.latest_unknown") || len(zero.Bars) != 30 {
			t.Fatalf("observed zero lost: %+v", zero)
		}
		for _, bar := range zero.Bars {
			if bar.Unknown || !bar.Zero || bar.Height != 2 {
				t.Fatalf("zero day is not explicit: %+v", bar)
			}
		}
		value.Complete, value.Commits, value.ActiveDays = false, 500, 1
		value.Daily[29].Count = 500
		partial := activityView(repository, mustDate("2026-08-30"), now, l)
		if partial.Summary != l.Textf("activity.minimum_counts", 500, 1) || !strings.Contains(partial.ChartLabel, l.Text("activity.incomplete")) || !partial.Bars[0].Unknown || partial.Bars[0].Zero || partial.Bars[29].Label != l.Textf("activity.day_minimum", 500) {
			t.Fatalf("partial sample masquerades as full activity: %+v", partial)
		}
		value.Complete, value.Daily = true, value.Daily[1:]
		if view := activityView(repository, mustDate("2026-08-30"), now, l); !view.Bars[0].Unknown {
			t.Fatal("missing daily cache entry was silently filled with zero")
		}
	}
}

func TestActivityShowsOwnTimeScopeAndOldCommits(t *testing.T) {
	l := newLocalizer(localeChinese)
	value := activityFixture()
	oldCommit := mustDate("2023-01-04")
	value.LatestCommitAt = &oldCommit
	repository := RepositoryMetric{ID: 101, FullName: "acme/radar", Activity: value}
	fresh := activityView(repository, mustDate("2026-08-30"), mustDate("2026-09-09"), l)
	if !strings.Contains(fresh.Heading, "最新开发活动") || !strings.Contains(fresh.Heading, "2026-09-08 12:30 UTC") || !strings.Contains(fresh.PeriodLabel, "2026-08-10") || !strings.Contains(fresh.LatestLabel, "2023-01-04") {
		t.Fatalf("latest activity was confused with historical observation: %+v", fresh)
	}
	stale := activityView(repository, mustDate("2026-08-30"), mustDate("2026-10-01"), l)
	if strings.Contains(stale.Heading, "最新开发活动") || !strings.Contains(stale.Heading, "截至 2026-09-08") {
		t.Fatalf("old cache exaggerated as current: %+v", stale)
	}
	// Shanghai's observation day has ended although the UTC date is unchanged.
	value.WindowEnd = time.Date(2026, 9, 8, 18, 0, 0, 0, time.UTC)
	value.FetchedAt = value.WindowEnd
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	observedDay := time.Date(2026, 9, 8, 0, 0, 0, 0, shanghai)
	if nextDay := activityView(repository, observedDay, value.FetchedAt, l); !strings.Contains(nextDay.Heading, "最新开发活动") {
		t.Fatal("activity later than the selected local date was presented as historical")
	}
	value.RepositoryID = 999
	if view := activityView(repository, mustDate("2026-08-30"), mustDate("2026-09-09"), l); view.HasCache {
		t.Fatal("another repository's activity was displayed")
	}
}

func TestActivityCardsStructureExistingBriefsAndKeepNavigation(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		t.Run(locale, func(t *testing.T) {
			queryer := populatedFake()
			item := &queryer.repositories.Items[0]
			item.Activity = activityFixture()
			item.GitHubCreatedAt = timePointer(mustDate("2022-08-30"))
			item.Analysis = &RepositoryAnalysis{SummaryZH: "整理研究人员关心的开源项目。", KeyPoints: []string{"自动同步真实 Star", "保存长期观察记录", "不在卡片展开的第三条能力"}, UseCases: []string{"开发者研究", "投资方向研究"}, Source: "codex"}
			body := request(t, newTestHandlerWithLocale(t, queryer, locale), "/repositories?date=2026-08-30&cursor=az&lang="+locale).Body.String()
			card := html.UnescapeString(feedCards(t, body)[0])
			l := newLocalizer(locale)
			for _, want := range []string{l.Text("activity.purpose"), l.Text("activity.capabilities"), l.Text("activity.use_cases"), item.Analysis.SummaryZH, "自动同步真实 Star", "投资方向研究", l.Textf("activity.counts", 6, 3), l.Text("activity.card_window"), l.Text("activity.source"), "4.0", "data-repository-detail", "return_to=", "data-reading-repository", "data-reading-toggle"} {
				if !strings.Contains(card, want) {
					t.Errorf("card missing %q", want)
				}
			}
			if strings.Contains(card, "不在卡片展开的第三条能力") || strings.Contains(card, "commit-bars") {
				t.Fatal("feed should remain a compact preview, with full text/chart in detail")
			}
		})
	}
}

func TestActivityDetailChartIsAccessibleAndEscapesFacts(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		queryer := populatedFake()
		value := activityFixture()
		value.DefaultBranch, value.IsFork, value.Complete = `<img src=x onerror=alert(1)>`, true, false
		queryer.repository.Repository.Activity = value
		queryer.repository.Analysis = &RepositoryAnalysis{SummaryZH: "原有用途不被重写。", KeyPoints: []string{strings.Repeat("完整说明", 70)}, UseCases: []string{`<script>alert(1)</script>`}}
		body := request(t, newTestHandlerWithLocale(t, queryer, locale), "/repositories/101?date=2026-08-30&lang="+locale).Body.String()
		l := newLocalizer(locale)
		for _, want := range []string{`role="img"`, `class="commit-bars"`, l.Text("activity.daily_details"), l.Text("activity.incomplete"), l.Text("activity.fork"), `rel="noopener noreferrer"`, "https://github.com/acme/radar/commits", "原有用途不被重写。", strings.Repeat("完整说明", 70), "&lt;script&gt;", "&lt;img"} {
			if !strings.Contains(body, want) {
				t.Errorf("detail missing %q", want)
			}
		}
		if strings.Contains(body, `<script>alert`) || strings.Contains(body, `<img src=x`) || strings.Count(body, `<rect class="commit-bar `) != 30 || strings.Count(body, `<li><time datetime="2026-`) != 30 {
			t.Fatal("daily chart lost accessibility or escaped content")
		}
	}
}

func TestBriefItemsDoesNotMutateOrBreakChineseText(t *testing.T) {
	input := []string{"  ", strings.Repeat("中文🧭", 50), "第二条", "第三条"}
	original := append([]string(nil), input...)
	preview := briefItems(input)
	if len(preview) != 2 || !utf8.ValidString(preview[0]) || len([]rune(preview[0])) != 65 || preview[1] != "第二条…" || !reflect.DeepEqual(input, original) {
		t.Fatalf("preview changed saved content or split a rune: %v", preview)
	}
}
