package web

import (
	"html"
	"regexp"
	"strings"
	"testing"
)

func TestLegacyResearchDoesNotPresentImportTimeAsTheOriginalReadingDate(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		t.Run(locale, func(t *testing.T) {
			queryer := populatedFake()
			analysis := &RepositoryAnalysis{
				SummaryZH: "这是一份从历史日报保留下来的项目研究说明。",
				Source:    "legacy_research", Revision: 1,
				AnalyzedAt: mustTime("2026-09-08T01:02:03Z"),
			}
			queryer.repositories.Items[0].Analysis = analysis
			queryer.repository.Analysis = analysis
			handler := newTestHandlerWithLocale(t, queryer, locale)
			l := newLocalizer(locale)
			card := feedCards(t, request(t, handler, "/repositories?view=all&lang="+locale).Body.String())[0]
			brief := regexp.MustCompile(`(?s)<section class="project-brief".*?</section>`).FindString(card)
			if brief == "" {
				t.Fatal("legacy card has no reading section")
			}
			brief = html.UnescapeString(brief)
			for _, expected := range []string{analysis.SummaryZH, l.Text("feed.saved_brief"), l.Text("feed.legacy_source"), l.Text("feed.legacy_date")} {
				if !strings.Contains(brief, expected) {
					t.Errorf("legacy brief missing %q", expected)
				}
			}
			if strings.Contains(brief, "2026-09-08") || strings.Contains(brief, l.Text("feed.ai_brief")) || strings.Contains(brief, l.Text("feed.not_read")) {
				t.Fatal("legacy card mislabeled an import date or the source of the archived reading")
			}
			detail := request(t, handler, "/repositories/101?lang="+locale).Body.String()
			provenance := html.UnescapeString(regexp.MustCompile(`(?s)<p class="analysis-provenance">.*?</p>`).FindString(detail))
			if !strings.Contains(detail, analysis.SummaryZH) || !strings.Contains(provenance, l.Text("feed.legacy_source")) || !strings.Contains(provenance, l.Text("feed.legacy_date")) {
				t.Fatal("detail omitted legacy research or its unknown original date")
			}
			if strings.Contains(provenance, "2026-09-08") {
				t.Fatal("detail presented this import date as the original analysis date")
			}
		})
	}
}
