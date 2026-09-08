package web

import (
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.gohtml static/*
var assets embed.FS

type Options struct {
	Logger           *slog.Logger
	Now              func() time.Time
	Location         *time.Location
	SiteName         string
	Locale           string
	Watcher          Watcher
	AllowLocalWrites bool
	WriteToken       string
}

type Handler struct {
	queryer          Queryer
	logger           *slog.Logger
	now              func() time.Time
	location         *time.Location
	siteName         string
	locale           string
	templates        map[string]map[string]*template.Template
	mux              *http.ServeMux
	static           fs.FS
	watcher          Watcher
	allowLocalWrites bool
	writeToken       string
	watchMu          sync.Mutex
	watchNonces      map[string]time.Time
	watchBusy        bool
}

// New creates the dashboard, including embedded assets and optional protected
// manual watch management. Reading the dashboard never requires credentials.
func New(queryer Queryer, options Options) (*Handler, error) {
	if queryer == nil {
		return nil, errors.New("web: nil Queryer")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Location == nil {
		var err error
		options.Location, err = time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return nil, fmt.Errorf("web: load timezone: %w", err)
		}
	}
	if strings.TrimSpace(options.SiteName) == "" {
		options.SiteName = "RepoTempo"
	}

	staticAssets, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("web: open static assets: %w", err)
	}

	h := &Handler{
		queryer:          queryer,
		logger:           options.Logger,
		now:              options.Now,
		location:         options.Location,
		siteName:         options.SiteName,
		locale:           normalizeLocale(options.Locale),
		static:           staticAssets,
		mux:              http.NewServeMux(),
		watcher:          options.Watcher,
		allowLocalWrites: options.AllowLocalWrites,
		writeToken:       options.WriteToken,
		watchNonces:      make(map[string]time.Time),
	}
	if err := h.parseTemplates(); err != nil {
		return nil, err
	}
	h.routes()
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if locale, ok := requestedLocale(r.URL.Query().Get("lang")); ok {
		cookie := &http.Cookie{
			Name:     localeCookieName,
			Value:    locale,
			Path:     "/",
			MaxAge:   365 * 24 * 60 * 60,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		}
		http.SetCookie(w, cookie)
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'")
	w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) routes() {
	h.mux.HandleFunc("GET /{$}", h.home)
	h.mux.HandleFunc("GET /repositories", h.repositories)
	h.mux.HandleFunc("GET /repositories/{id}", h.repository)
	h.mux.HandleFunc("GET /topics", h.topics)
	h.mux.HandleFunc("GET /topics/{slug}", h.topic)
	h.mux.HandleFunc("GET /discoveries", h.discoveries)
	h.mux.HandleFunc("GET /runs", h.runs)
	h.mux.HandleFunc("GET /watch", h.watchForm)
	h.mux.HandleFunc("GET /watch/new", h.watchForm)
	h.mux.HandleFunc("POST /watch", h.watchAdd)
	h.mux.HandleFunc("GET /watch/imports/{id}", h.importTask)
	h.mux.HandleFunc("GET /watch/imports/{id}/status", h.importTaskStatus)
	h.mux.HandleFunc("GET /healthz", h.health)
	h.mux.HandleFunc("GET /readyz", h.ready)
	h.mux.HandleFunc("GET /static/tokens.css", h.staticAsset("tokens.css", "text/css; charset=utf-8"))
	h.mux.HandleFunc("GET /static/app.css", h.staticAsset("app.css", "text/css; charset=utf-8"))
	h.mux.HandleFunc("GET /static/feed.css", h.staticAsset("feed.css", "text/css; charset=utf-8"))
	h.mux.HandleFunc("GET /static/app.js", h.staticAsset("app.js", "text/javascript; charset=utf-8"))
	h.mux.HandleFunc("GET /static/reading-state.js", h.staticAsset("reading-state.js", "text/javascript; charset=utf-8"))
	h.mux.HandleFunc("GET /static/reading-position.js", h.staticAsset("reading-position.js", "text/javascript; charset=utf-8"))
	h.mux.HandleFunc("GET /static/imports.js", h.staticAsset("imports.js", "text/javascript; charset=utf-8"))
	h.mux.HandleFunc("GET /static/imports.css", h.staticAsset("imports.css", "text/css; charset=utf-8"))
	h.mux.HandleFunc("GET /", h.notFound)
}

func (h *Handler) parseTemplates() error {
	h.templates = make(map[string]map[string]*template.Template, 2)
	for _, locale := range []string{localeEnglish, localeChinese} {
		localized := newLocalizer(locale)
		unavailable := localized.Text("page.not_available")
		funcs := template.FuncMap{
			"formatInt":    formatInt,
			"formatIntPtr": func(value *int64) string { return formatIntPtrLocalized(value, unavailable) },
			"formatSigned": func(value *int64) string { return formatSignedLocalized(value, unavailable) },
			"formatDecimalSigned": func(value *float64) string {
				return formatDecimalSignedLocalized(value, unavailable)
			},
			"formatDate":      func(value time.Time) string { return formatDateLocalized(value, h.location, unavailable) },
			"formatDatePtr":   func(value *time.Time) string { return formatDatePtrLocalized(value, h.location, unavailable) },
			"formatDateTime":  func(value time.Time) string { return formatDateTimeLocalized(value, h.location, unavailable) },
			"formatTimePtr":   func(value *time.Time) string { return formatTimePtr(value, h.location, localized.Text("time.running")) },
			"formatPercent":   func(value *float64) string { return formatPercentLocalized(value, unavailable) },
			"coveragePercent": coveragePercent,
			"formatDuration": func(start time.Time, finish *time.Time) string {
				return formatDuration(start, finish, localized.Text("time.running"), unavailable)
			},
			"statusLabel":      localized.StatusLabel,
			"statusClass":      statusClass,
			"deltaClass":       deltaClass,
			"floatDeltaClass":  floatDeltaClass,
			"sourceLabel":      localized.SourceLabel,
			"trendingPeriod":   localized.TrendingPeriodLabel,
			"trendingMessage":  localized.TrendingMessage,
			"topicName":        localized.TopicName,
			"t":                localized.Text,
			"tf":               localized.Textf,
			"githubURL":        githubURL,
			"briefText":        briefText,
			"projectGitHubURL": projectGitHubURL,
			"aiAnalysis":       aiAnalysis,
			"analysisSource":   analysisSource,
			"tagOptionLabel":   func(tag string) string { return tagOptionLabel(tag, locale) },
			"join":             strings.Join,
			"lower":            strings.ToLower,
			"watchText":        func(key string) string { return watchText(locale, key) },
			"importText":       func(key string) string { return importText(locale, key) },

			"briefItems": briefItems,
			"repositoryAge": func(created *time.Time, asOf time.Time) repositoryAgeView {
				return repositoryAge(created, asOf, h.location, localized)
			},
			"activityView": func(repository RepositoryMetric, asOf time.Time) activityPresentation {
				return activityView(repository, asOf, h.now(), localized)
			},
		}
		h.templates[locale] = make(map[string]*template.Template)
		for _, page := range []string{"home", "repositories", "repository", "topics", "topic", "runs", "error", "watch", "import"} {
			tmpl, err := template.New("base.gohtml").Funcs(funcs).ParseFS(
				assets,
				"templates/base.gohtml",
				"templates/"+page+".gohtml",
			)
			if err != nil {
				return fmt.Errorf("web: parse %s template for %s: %w", page, locale, err)
			}
			h.templates[locale][page] = tmpl
		}
	}
	return nil
}

func (h *Handler) staticAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		contents, err := fs.ReadFile(h.static, name)
		if err != nil {
			h.notFound(w, r)
			return
		}
		checksum := sha256.Sum256(contents)
		etag := fmt.Sprintf(`"%x"`, checksum[:8])
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", contentType)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(contents)
	}
}

func githubURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return ""
	}
	return parsed.String()
}
