package web

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFeedCardsKeepProjectIdentityEvidenceAndReadingSourceTogether(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		t.Run(locale, func(t *testing.T) {
			queryer := populatedFake()
			first := queryer.repositories.Items[0]
			first.ID, first.FullName = 201, "team/first-agent"
			first.HTMLURL = "https://github.com/unrelated/repository"
			first.Description = "The first project's original description."
			first.IsNew, first.IsFocus = true, true
			first.FirstSeenSource = "github_trending"
			first.GitHubCreatedAt = timePointer(mustDate("2025-04-03"))
			first.Analysis = &RepositoryAnalysis{SummaryZH: "这个项目为研究人员保存长期记忆。", Source: "codex", Model: "gpt-6", AnalyzedAt: mustDate("2026-08-29")}
			second := first
			second.ID, second.FullName = 202, "team/second-agent"
			second.Description = "The second project's own description."
			second.IsNew, second.IsFocus = false, false
			second.Analysis = &RepositoryAnalysis{SummaryZH: "这个项目专门整理浏览器任务。", Source: "kimi-code", Model: "kimi-k2", AnalyzedAt: mustDate("2026-08-28")}
			queryer.repositories.Items, queryer.repositories.Total = []RepositoryMetric{first, second}, 2
			response := request(t, newTestHandlerWithLocale(t, queryer, locale), "/repositories?date=2026-08-30&lang="+locale)
			if response.Code != http.StatusOK {
				t.Fatalf("feed status = %d", response.Code)
			}
			cards := feedCards(t, response.Body.String())
			if len(cards) != 2 {
				t.Fatalf("got %d cards for two project entries", len(cards))
			}
			l := newLocalizer(locale)
			for index, item := range []RepositoryMetric{first, second} {
				card := html.UnescapeString(cards[index])
				reader := "Codex"
				wantBadges := 2
				if index == 1 {
					reader, wantBadges = "Kimi", 0
				}
				for _, want := range []string{
					fmt.Sprintf(`aria-labelledby="project-%d"`, item.ID), item.FullName, item.Description,
					item.Analysis.SummaryZH, item.Analysis.Model, item.Analysis.AnalyzedAt.Format("2006-01-02"),
					l.Text("feed.ai_brief"), l.Text("feed.added"), item.FirstSeenAt.Format("2006-01-02"),
					l.Text("feed.created"), "2025-04-03", "GitHub Trending", item.PrimaryLanguage,
					l.Textf("feed.reading_source", reader), l.TopicName("ai-agent", "General agents"),
					"12,450", "+430", "3.6%", "+3 places", "7", "4",
					fmt.Sprintf(`/repositories/%d?date=2026-08-30&lang=%s`, item.ID, locale),
					`href="https://github.com/` + item.FullName + `" target="_blank" rel="noopener noreferrer"`,
				} {
					if locale == localeChinese && want == "+3 places" {
						want = "+3 位"
					}
					if !strings.Contains(card, want) {
						t.Errorf("card %d missing %q", item.ID, want)
					}
				}
				if strings.Count(card, `class="new-badge"`) != wantBadges {
					t.Fatalf("card %d did not preserve its new/watchlist states", item.ID)
				}
				other := first
				if index == 0 {
					other = second
				}
				if strings.Contains(card, other.Analysis.SummaryZH) || strings.Contains(card, "https://github.com/unrelated/repository") {
					t.Fatalf("card %d contains another project's evidence or noncanonical GitHub URL", item.ID)
				}
			}
		})
	}
}

func TestFeedDistinguishesAIBriefsManualNotesAndMissingAnalysis(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		for _, test := range []struct {
			name     string
			analysis *RepositoryAnalysis
			kind     string
		}{
			{"codex", &RepositoryAnalysis{SummaryZH: "来自 Codex 的项目简读。", Source: "codex"}, "ai"},
			{"kimi", &RepositoryAnalysis{SummaryZH: "来自 Kimi 的项目简读。", Source: "kimi-code"}, "ai"},
			{"manual", &RepositoryAnalysis{SummaryZH: "我手动记录的项目说明。", Source: "manual_note"}, "manual"},
			{"nil", nil, "missing"},
			{"empty", &RepositoryAnalysis{Source: "codex"}, "missing"},
			{"whitespace", &RepositoryAnalysis{SummaryZH: " \n\t ", Source: "codex"}, "missing"},
		} {
			t.Run(locale+"/"+test.name, func(t *testing.T) {
				queryer := populatedFake()
				queryer.repositories.Items[0].Analysis = test.analysis
				card := html.UnescapeString(feedCards(t, request(t, newTestHandlerWithLocale(t, queryer, locale), "/repositories").Body.String())[0])
				l := newLocalizer(locale)
				if test.kind == "missing" {
					if !strings.Contains(card, l.Text("feed.not_read")) || !strings.Contains(card, l.Text("feed.pending_help")) || strings.Contains(card, `class="project-brief-text"`) {
						t.Fatal("missing or blank analysis must be presented as not reviewed")
					}
					return
				}
				if !strings.Contains(card, test.analysis.SummaryZH) || strings.Contains(card, l.Text("feed.not_read")) {
					t.Fatal("saved analysis was omitted or marked unreviewed")
				}
				if test.kind == "manual" {
					if !strings.Contains(card, l.Text("feed.saved_brief")) || strings.Contains(card, l.Text("feed.ai_brief")) {
						t.Fatal("manual note was mislabeled as AI output")
					}
				} else if !strings.Contains(card, l.Text("feed.ai_brief")) || strings.Contains(card, l.Text("feed.saved_brief")) {
					t.Fatal("AI reading source lost its label")
				}
			})
		}
	}
}

func TestFeedBriefTruncationPreservesChineseRunesAndEscapesHostileText(t *testing.T) {
	long := strings.Repeat("中文观察🧭", 90)
	short := briefText(long)
	if !utf8.ValidString(short) || len([]rune(short)) != 281 || short != string([]rune(long)[:280])+"…" {
		t.Fatal("long Chinese summary was not truncated at a valid rune boundary")
	}
	queryer := populatedFake()
	item := &queryer.repositories.Items[0]
	item.FullName = `<script>alert("project")</script>`
	item.HTMLURL = "javascript:alert(1)"
	item.Description = `<img src=x onerror="alert('description')">`
	item.Analysis = &RepositoryAnalysis{SummaryZH: `<script>alert("analysis")</script> ` + long, Source: "codex", Model: `<img src=x onerror="alert('model')">`}
	body := request(t, newTestHandler(t, queryer), "/repositories").Body.String()
	if !utf8.ValidString(body) || strings.Contains(body, "�") || strings.Contains(body, `<script>alert(`) || strings.Contains(body, `<img src=x`) || strings.Contains(body, "javascript:alert") {
		t.Fatal("feed introduced invalid UTF-8 or active hostile markup")
	}
	for _, escaped := range []string{"&lt;script&gt;", "&lt;img", "…"} {
		if !strings.Contains(body, escaped) {
			t.Errorf("feed missing safely displayed text %q", escaped)
		}
	}
}

func TestFeedRequestsTwentyEntriesAndPreservesDetailDateAndLanguage(t *testing.T) {
	queryer := populatedFake()
	first := queryer.repositories.Items[0]
	queryer.repositories.Items = nil
	for index := 0; index < 20; index++ {
		item := first
		item.ID = int64(500 + index)
		item.FullName = fmt.Sprintf("team/repo-%d", index)
		queryer.repositories.Items = append(queryer.repositories.Items, item)
	}
	queryer.repositories.Total, queryer.repositories.HasMore, queryer.repositories.NextCursor = 21, true, "ef"
	handler := newTestHandler(t, queryer)
	body := request(t, handler, "/repositories?date=2026-08-30&period=7d&focus=1&lang=zh-CN").Body.String()
	if queryer.lastRepositoryQuery.Limit != 20 || len(feedCards(t, body)) != 20 {
		t.Fatalf("reading feed must request and render a page of 20; query limit=%d", queryer.lastRepositoryQuery.Limit)
	}
	links := regexp.MustCompile(`href="(/repositories/[0-9]+[^\"]*)"`).FindAllStringSubmatch(body, -1)
	if len(links) != 40 {
		t.Fatalf("expected title and detail links for each of 20 projects; got %d", len(links))
	}
	for _, match := range links {
		location, err := url.Parse(html.UnescapeString(match[1]))
		if err != nil || location.Query().Get("date") != "2026-08-30" || location.Query().Get("lang") != localeChinese {
			t.Fatalf("detail URL lost observation context: %q", match[1])
		}
	}
	if !strings.Contains(html.UnescapeString(body), `/repositories?cursor=ef&date=2026-08-30&focus=1&lang=zh-CN&period=7d`) {
		t.Fatal("next page did not preserve the selected date and filters")
	}
	response := request(t, handler, html.UnescapeString(links[0][1]))
	if response.Code != http.StatusOK || queryer.lastRepositoryAsOf.Format("2006-01-02") != "2026-08-30" {
		t.Fatal("detail handler did not receive the date from the feed")
	}
}

func feedCards(t *testing.T, body string) []string {
	t.Helper()
	cards := regexp.MustCompile(`(?s)<article class="project-card"[^>]*>.*?</article>`).FindAllString(body, -1)
	if len(cards) == 0 {
		t.Fatal("no project cards rendered")
	}
	return cards
}
