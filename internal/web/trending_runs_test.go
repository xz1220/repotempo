package web

import (
	"html"
	"net/http"
	"strings"
	"testing"
)

func TestRunsShowTrendingWindowsAndPreserveSearchEvidence(t *testing.T) {
	for _, locale := range []string{localeEnglish, localeChinese} {
		queryer := populatedFake()
		queryer.runs.Items[0].TrendingWindows = []TrendingWindowStatus{
			{Period: "daily", Count: 25},
			{Period: "weekly", Count: 0, Error: `Upstream <script>alert("unsafe")</script> failed at https://source.example/private?credential=hidden`},
			{Period: "monthly", Count: 18},
		}
		response := request(t, newTestHandlerWithLocale(t, queryer, locale), "/runs")
		if response.Code != http.StatusOK {
			t.Fatalf("run history status = %d", response.Code)
		}
		body := response.Body.String()
		readable := html.UnescapeString(body)
		l := newLocalizer(locale)
		for _, want := range []string{`class="run-trending-status"`, l.Text("runs.trending_daily"), l.Text("runs.trending_weekly"), l.Text("runs.trending_monthly"), l.Textf("runs.trending_entries", "25"), l.Textf("runs.trending_entries", "0"), l.Textf("runs.trending_entries", "18"), l.Text("runs.search_api"), l.Text("runs.core_api"), l.Text("runs.search_integrity"), l.Text("runs.trending_url_omitted")} {
			if !strings.Contains(readable, want) {
				t.Errorf("%s run details missing %q", locale, want)
			}
		}
		if strings.Contains(body, `<script>alert`) || !strings.Contains(body, "&lt;script&gt;") || strings.Contains(body, "source.example") || strings.Contains(body, "credential=hidden") {
			t.Fatal("Trending evidence propagated active markup or an upstream URL")
		}
	}
}

func TestRunsShowSkippedTrendingOnlyWhenRecorded(t *testing.T) {
	for _, locale := range []string{localeEnglish, localeChinese} {
		queryer := populatedFake()
		handler := newTestHandlerWithLocale(t, queryer, locale)
		body := request(t, handler, "/runs").Body.String()
		if strings.Contains(body, `class="run-trending-status"`) {
			t.Fatal("legacy run without Trending data fabricated a Trending status")
		}
		queryer.runs.Items[0].TrendingSkipped = true
		queryer.runs.Items[0].TrendingSkipReason = `Already collected <today> https://untrusted.example/run`
		body = request(t, handler, "/runs").Body.String()
		if !strings.Contains(body, newLocalizer(locale).Text("runs.trending_skipped")) || !strings.Contains(body, "Already collected &lt;today&gt;") || strings.Contains(body, "untrusted.example") {
			t.Fatal("recorded skip state was missing or unsafe")
		}
	}
}
