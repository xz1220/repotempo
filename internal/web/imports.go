package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

var ErrImportQueueBusy = errors.New("import: queue full or conflicting pending task")
var importIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{15,100}$`)

func validImportID(id string) bool { return importIDPattern.MatchString(id) }

type Importer interface {
	SubmitImport(context.Context, WatchRequest) (ImportStatus, error)
	GetImportStatus(context.Context, string) (ImportStatus, error)
}

type RepositoryReadme struct {
	Intro, HTMLURL, SHA, Path string
	Headings                  []string
	FetchedAt                 time.Time
	Truncated                 bool
}

type ImportStatus struct {
	ID, Repository, FullName, Stage, ErrorCode string
	RepositoryID                               int64
	Focus, Created                             bool
	Attempts                                   int
	QueuedAt, UpdatedAt                        time.Time
	NextAttemptAt                              *time.Time
	Readme                                     *RepositoryReadme
}

func (value ImportStatus) Terminal() bool {
	return value.Stage == "done" || value.Stage == "partial" || value.Stage == "failed"
}

type importPresentation struct {
	Status                    ImportStatus
	Label, Message, DetailURL string
	RetryURL, PollURL         string
	Terminal                  bool
}

type importPageView struct {
	pageView
	Import importPresentation
}

func importText(locale, key string) string {
	if pair, ok := importMessages[key]; ok {
		if locale == localeChinese {
			return pair[0]
		}
		return pair[1]
	}
	return importText(locale, "unavailable")
}

var importMessages = map[string][2]string{
	"title":            {"导入进度", "Import progress"},
	"description":      {"入库与关注是两件事。任务在后台继续处理，无需停留在此页面。", "Importing and following are separate. This task continues in the background if you leave this page."},
	"queued":           {"等待处理", "Queued"},
	"resolving":        {"获取项目并入库", "Fetching and importing the project"},
	"reading":          {"读取 README", "Reading README"},
	"classifying":      {"解析项目资料", "Processing project metadata"},
	"done":             {"导入完成", "Import complete"},
	"partial":          {"已入库，部分资料未能读取", "Imported; some information is unavailable"},
	"failed":           {"导入失败", "Import failed"},
	"retrying":         {"请求暂时受限或失败，任务会自动重试。", "A request was limited or failed temporarily. The task will retry automatically."},
	"unavailable":      {"资料暂不可用，请稍后重试。已保存的项目数据不会丢失。", "Information is temporarily unavailable. Saved project data is preserved."},
	"waiting":          {"可以返回浏览其他项目，稍后再查看进度。", "You can browse other projects and check progress later."},
	"completed":        {"已保存项目资料、首个 Star 观测和 README 原文摘录。已有 AI 简读、备注及历史均保留。", "Project metadata, an initial Star observation and an original README extract are saved. Existing AI readings, notes and history are preserved."},
	"open":             {"查看项目", "View project"},
	"refresh":          {"刷新进度", "Refresh progress"},
	"retry":            {"重新导入", "Import again"},
	"back":             {"返回 GitHub 项目", "Back to GitHub projects"},
	"followed":         {"本次导入同时设为我的关注", "Also requested adding this project to My watchlist"},
	"not_followed":     {"本次仅导入，不改变已有关注状态", "Import only; existing watchlist choices are preserved"},
	"readme":           {"README 原文摘录", "Original README excerpt"},
	"readme_help":      {"程序提取的项目自述，不是 AI 解读，也未验证项目能力。", "A program-extracted project description, not AI analysis or verification of its capabilities."},
	"readme_open":      {"阅读 GitHub README", "Read README on GitHub"},
	"readme_truncated": {"原文较长，此处为节选。", "The source is long; this is an excerpt."},
	"updated":          {"更新于", "Updated"},
	"poll_paused":      {"自动刷新已暂停，可点击刷新进度。任务仍在后台处理。", "Automatic refresh paused. Refresh progress manually; the task continues in the background."},
	"empty_focus":      {"还没有关注项目", "No followed projects yet"},
	"empty_focus_help": {"导入时勾选“同时设为我的关注”，项目就会出现在这里。仅导入的项目不会加入关注。", "Select “Also add to My watchlist” when importing. Importing alone does not follow a project."},
}

func (h *Handler) importPresentation(status ImportStatus, locale string) importPresentation {
	view := importPresentation{Status: status, Label: importText(locale, status.Stage), Message: importText(locale, "waiting"), Terminal: status.Terminal()}
	values := url.Values{"lang": {locale}}
	view.PollURL = "/watch/imports/" + status.ID + "/status?" + values.Encode()
	if status.RepositoryID > 0 {
		view.DetailURL = "/repositories/" + strconv.FormatInt(status.RepositoryID, 10) + "?" + values.Encode()
	}
	if status.NextAttemptAt != nil && !view.Terminal {
		view.Message = importText(locale, "retrying")
	} else if status.Stage == "done" {
		view.Message = importText(locale, "completed")
	} else if view.Terminal {
		view.Message = importText(locale, "unavailable")
	}
	values.Set("repository", status.Repository)
	if status.Focus {
		values.Set("focus", "1")
	}
	view.RetryURL = "/watch/new?" + values.Encode()
	return view
}

func (h *Handler) importTask(w http.ResponseWriter, r *http.Request) {
	h.serveImport(w, r, false)
}

func (h *Handler) importTaskStatus(w http.ResponseWriter, r *http.Request) {
	h.serveImport(w, r, true)
}

func (h *Handler) serveImport(w http.ResponseWriter, r *http.Request, asJSON bool) {
	id := r.PathValue("id")
	importer, ok := h.watcher.(Importer)
	if !ok || !validImportID(id) {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	status, err := importer.GetImportStatus(ctx, id)
	if err != nil || status.ID != id {
		code := http.StatusServiceUnavailable
		if errors.Is(err, ErrNotFound) || (err == nil && status.ID != id) {
			code = http.StatusNotFound
		}
		http.Error(w, importText(h.localeFor(r), "unavailable"), code)
		return
	}
	locale := h.localeFor(r)
	view := h.importPresentation(status, locale)
	w.Header().Set("Cache-Control", "no-store")
	if asJSON {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(struct {
			Stage    string `json:"stage"`
			Label    string `json:"label"`
			Message  string `json:"message"`
			Terminal bool   `json:"terminal"`
		}{status.Stage, view.Label, view.Message, view.Terminal})
		return
	}
	page := importPageView{pageView: pageView{Meta: h.metaText(h.localizerFor(r), importText(locale, "title"), importText(locale, "description"), "repositories", nil)}, Import: view}
	page.Meta.Locale, page.Meta.EnglishURL, page.Meta.ChineseURL = locale, languageURL(r, localeEnglish), languageURL(r, localeChinese)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", locale)
	if err := h.templates[locale]["import"].ExecuteTemplate(w, "base", page); err != nil {
		h.logger.ErrorContext(ctx, "import status template failed", "error", err)
	}
}
