package web

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

//go:embed templates/*.gohtml static/*
var assets embed.FS

type Options struct {
	Logger   *slog.Logger
	Now      func() time.Time
	Location *time.Location
	SiteName string
}

type Handler struct {
	queryer   Queryer
	logger    *slog.Logger
	now       func() time.Time
	location  *time.Location
	siteName  string
	templates map[string]*template.Template
	mux       *http.ServeMux
	static    fs.FS
}

// New creates the complete read-only dashboard handler, including embedded
// assets, health checks, and security headers.
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
		options.SiteName = "GitHub Radar"
	}

	staticAssets, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("web: open static assets: %w", err)
	}

	h := &Handler{
		queryer:  queryer,
		logger:   options.Logger,
		now:      options.Now,
		location: options.Location,
		siteName: options.SiteName,
		static:   staticAssets,
		mux:      http.NewServeMux(),
	}
	if err := h.parseTemplates(); err != nil {
		return nil, err
	}
	h.routes()
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	h.mux.HandleFunc("GET /healthz", h.health)
	h.mux.HandleFunc("GET /readyz", h.ready)
	h.mux.HandleFunc("GET /static/app.css", h.staticAsset("app.css", "text/css; charset=utf-8"))
	h.mux.HandleFunc("GET /static/app.js", h.staticAsset("app.js", "text/javascript; charset=utf-8"))
	h.mux.HandleFunc("GET /", h.notFound)
}

func (h *Handler) parseTemplates() error {
	h.templates = make(map[string]*template.Template)
	funcs := template.FuncMap{
		"formatInt":       formatInt,
		"formatIntPtr":    formatIntPtr,
		"formatSigned":    formatSigned,
		"formatDate":      func(value time.Time) string { return formatDate(value, h.location) },
		"formatDatePtr":   func(value *time.Time) string { return formatDatePtr(value, h.location) },
		"formatDateTime":  func(value time.Time) string { return formatDateTime(value, h.location) },
		"formatTimePtr":   func(value *time.Time) string { return formatTimePtr(value, h.location) },
		"formatPercent":   formatPercent,
		"coveragePercent": coveragePercent,
		"formatDuration":  formatDuration,
		"statusLabel":     statusLabel,
		"statusClass":     statusClass,
		"sourceLabel":     sourceLabel,
		"githubURL":       githubURL,
		"join":            strings.Join,
		"lower":           strings.ToLower,
	}
	for _, page := range []string{"home", "repositories", "repository", "topics", "topic", "discoveries", "runs", "error"} {
		tmpl, err := template.New("base.gohtml").Funcs(funcs).ParseFS(
			assets,
			"templates/base.gohtml",
			"templates/"+page+".gohtml",
		)
		if err != nil {
			return fmt.Errorf("web: parse %s template: %w", page, err)
		}
		h.templates[page] = tmpl
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
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Content-Type", contentType)
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
