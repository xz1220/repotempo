package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

const testImportID = "12345678-1234-4234-8234-123456789abc"

type fakeImporter struct {
	fakeWatcher
	job                   ImportStatus
	queued                int
	queueError, readError error
}

func (f *fakeImporter) SubmitImport(ctx context.Context, request WatchRequest) (ImportStatus, error) {
	if _, ok := ctx.Deadline(); !ok {
		panic("queue write needs a deadline")
	}
	f.queued++
	f.input = request
	return f.job, f.queueError
}
func (f *fakeImporter) GetImportStatus(context.Context, string) (ImportStatus, error) {
	return f.job, f.readError
}

func importHandler(t *testing.T, importer *fakeImporter, public bool) *Handler {
	t.Helper()
	opts := Options{Watcher: importer, AllowLocalWrites: !public}
	if public {
		opts.WriteToken = "fixture-management-token-not-a-secret"
	}
	h, err := New(populatedFake(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestImportFormDefaultsToUnfollowedAndReturnsDurableTask(t *testing.T) {
	for _, focus := range []bool{false, true} {
		importer := &fakeImporter{job: ImportStatus{ID: testImportID, Stage: "queued"}}
		h := importHandler(t, importer, false)
		base := "http://127.0.0.1:8878"
		formRequest := httptest.NewRequest(http.MethodGet, base+"/watch/new?lang=zh-CN", nil)
		formRequest.RemoteAddr = "127.0.0.1:12345"
		formResponse := httptest.NewRecorder()
		h.ServeHTTP(formResponse, formRequest)
		form := formResponse.Body.String()
		checkbox := regexp.MustCompile(`<input[^>]*id="import-focus"[^>]*>`).FindString(form)
		if checkbox == "" || strings.Contains(checkbox, "checked") || !strings.Contains(form, "导入 GitHub 项目") {
			t.Fatal("import form implicitly follows projects")
		}
		cookie := watchFormCookie(t, h, base)
		values := url.Values{"repository": {"https://github.com/owner/repo"}, "csrf_token": {cookie.Value}}
		if focus {
			values.Set("focus", "1")
		}
		req := httptest.NewRequest(http.MethodPost, base+"/watch?lang=zh-CN", strings.NewReader(values.Encode()))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", base)
		req.AddCookie(cookie)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/watch/imports/"+testImportID+"?lang=zh-CN" || importer.queued != 1 || importer.calls != 0 || importer.input.Focus != focus {
			t.Fatalf("not a separate async import: %d %+v", response.Code, importer)
		}
	}
}

func TestImportStatusIsSafeReadOnlyAndDistinguishesPartial(t *testing.T) {
	for _, stage := range []string{"queued", "reading", "done", "partial", "failed"} {
		importer := &fakeImporter{job: ImportStatus{ID: testImportID, Stage: stage, Repository: "owner/repo", RepositoryID: 99, UpdatedAt: time.Now(), Readme: &RepositoryReadme{Intro: `<img src=x onerror=alert(1)>`, HTMLURL: "https://evil.example/readme"}}}
		h := importHandler(t, importer, false)
		page := request(t, h, "/watch/imports/"+testImportID+"?lang=zh-CN")
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "&lt;img") || strings.Contains(page.Body.String(), `href="https://evil.example`) || !strings.Contains(page.Body.String(), "不是 AI 解读") {
			t.Fatalf("unsafe or misleading status: %d", page.Code)
		}
		response := request(t, h, "/watch/imports/"+testImportID+"/status?lang=zh-CN")
		var result struct {
			Stage    string
			Terminal bool
			Label    string
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Stage != stage || result.Terminal != importer.job.Terminal() || result.Label == "" || importer.queued != 0 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("wrong read-only status: %+v", result)
		}
		if strings.Contains(response.Body.String(), "Readme") || strings.Contains(response.Body.String(), "lease") || strings.Contains(response.Body.String(), "operator_token") {
			t.Fatal("status leaked internal state")
		}
	}
}

func TestImportFailuresKeepExistingAuthorizationAndNonceChecks(t *testing.T) {
	for _, item := range []struct {
		err  error
		code int
	}{{ErrImportQueueBusy, 429}, {ErrWatchInvalid, 400}, {ErrWatchUnavailable, 502}} {
		importer := &fakeImporter{queueError: item.err}
		h := importHandler(t, importer, true)
		base := "https://radar.example"
		cookie := watchFormCookie(t, h, base)
		bad := httptest.NewRecorder()
		h.ServeHTTP(bad, watchPOST(base, cookie, "incorrect"))
		if bad.Code != 403 || importer.queued != 0 {
			t.Fatal("unauthorized import reached queue")
		}
		response := httptest.NewRecorder()
		h.ServeHTTP(response, watchPOST(base, cookie, "fixture-management-token-not-a-secret"))
		if response.Code != item.code || importer.queued != 1 || strings.Contains(response.Body.String(), "fixture-management-token") {
			t.Fatalf("bad import error status: %d", response.Code)
		}
		replay := httptest.NewRecorder()
		h.ServeHTTP(replay, watchPOST(base, cookie, "fixture-management-token-not-a-secret"))
		if replay.Code != 409 || importer.queued != 1 {
			t.Fatal("replayed import was accepted")
		}
	}
	for _, item := range []struct {
		err  error
		code int
	}{{ErrNotFound, 404}, {errors.New("internal-secret"), 503}} {
		importer := &fakeImporter{readError: item.err}
		response := request(t, importHandler(t, importer, false), "/watch/imports/"+testImportID)
		if response.Code != item.code || strings.Contains(response.Body.String(), "internal-secret") {
			t.Fatal("task read error was hidden or leaked")
		}
	}
}

func TestReadmeFallbackNeverReplacesSavedAIReading(t *testing.T) {
	queryer := populatedFake()
	readme := &RepositoryReadme{Intro: "这是一段有明确来源的项目自述。", HTMLURL: "https://github.com/acme/radar/blob/main/README.md", FetchedAt: time.Now()}
	queryer.repositories.Items[0].Analysis = nil
	queryer.repositories.Items[0].Readme = readme
	queryer.repository.Analysis = nil
	queryer.repository.Repository.Readme = readme
	h := newTestHandler(t, queryer)
	for _, path := range []string{"/repositories?lang=zh-CN", "/repositories/101?lang=zh-CN"} {
		body := request(t, h, path).Body.String()
		if !strings.Contains(body, readme.Intro) || !strings.Contains(body, "README 原文摘录") {
			t.Fatalf("README evidence missing: %s", path)
		}
	}
	queryer.repositories.Items[0].Analysis = &RepositoryAnalysis{SummaryZH: "已有的AI解读应保持优先展示。", Source: "codex"}
	body := request(t, h, "/repositories?lang=zh-CN").Body.String()
	if !strings.Contains(body, "已有的AI解读") || strings.Contains(body, readme.Intro) {
		t.Fatal("README fallback displaced a saved interpretation")
	}
}
