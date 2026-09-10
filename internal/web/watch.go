package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const watchCookie = "github_radar_watch_csrf"

func (h *Handler) watchForm(w http.ResponseWriter, r *http.Request) {
	input := WatchRequest{Repository: r.URL.Query().Get("repository"), Focus: r.URL.Query().Get("focus") == "1"}
	if len(input.Repository) > 300 {
		input.Repository = ""
	}
	h.renderWatch(w, r, http.StatusOK, input, "")
}

func (h *Handler) watchAdd(w http.ResponseWriter, r *http.Request) {
	if !watchSameOrigin(r) {
		h.renderWatch(w, r, http.StatusForbidden, WatchRequest{}, "forbidden")
		return
	}
	writable, requiresToken := h.watchAccess(r)
	if !writable {
		h.renderWatch(w, r, http.StatusForbidden, WatchRequest{}, "read_only")
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/x-www-form-urlencoded" {
		h.renderWatch(w, r, http.StatusUnsupportedMediaType, WatchRequest{}, "invalid")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 24*1024)
	if err := r.ParseForm(); err != nil {
		h.renderWatch(w, r, http.StatusRequestEntityTooLarge, WatchRequest{}, "too_large")
		return
	}
	input := WatchRequest{Repository: strings.TrimSpace(r.PostForm.Get("repository")), TopicSlug: strings.TrimSpace(r.PostForm.Get("topic")), Note: strings.TrimSpace(r.PostForm.Get("note")), Focus: r.PostForm.Get("focus") == "1"}
	if values := r.PostForm["focus"]; len(values) > 1 || (len(values) == 1 && values[0] != "0" && values[0] != "1") {
		h.renderWatch(w, r, http.StatusBadRequest, input, "invalid")
		return
	}
	if len(input.Repository) > 300 || len(input.TopicSlug) > 100 || utf8.RuneCountInString(input.Note) > 2000 {
		h.renderWatch(w, r, http.StatusBadRequest, WatchRequest{}, "invalid")
		return
	}
	if requiresToken && !watchTokenEqual(h.writeToken, r.PostForm.Get("operator_token")) {
		h.renderWatch(w, r, http.StatusForbidden, input, "forbidden")
		return
	}
	if !h.consumeWatchNonce(r, r.PostForm.Get("csrf_token")) {
		h.renderWatch(w, r, http.StatusConflict, input, "csrf")
		return
	}
	h.watchMu.Lock()
	if h.watchBusy {
		h.watchMu.Unlock()
		h.renderWatch(w, r, http.StatusTooManyRequests, input, "busy")
		return
	}
	h.watchBusy = true
	h.watchMu.Unlock()
	defer func() { h.watchMu.Lock(); h.watchBusy = false; h.watchMu.Unlock() }()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if importer, ok := h.watcher.(Importer); ok {
		job, err := importer.SubmitImport(ctx, input)
		if err != nil {
			status, key := watchError(err)
			h.renderWatch(w, r, status, input, key)
			return
		}
		if !validImportID(job.ID) {
			h.renderWatch(w, r, http.StatusBadGateway, input, "unavailable")
			return
		}
		values := url.Values{"lang": {h.localeFor(r)}}
		http.Redirect(w, r, "/watch/imports/"+job.ID+"?"+values.Encode(), http.StatusSeeOther)
		return
	}
	result, err := h.watcher.AddWatch(ctx, input)
	if err != nil {
		status, key := watchError(err)
		h.renderWatch(w, r, status, input, key)
		return
	}
	if result.ID <= 0 {
		h.renderWatch(w, r, http.StatusBadGateway, input, "unavailable")
		return
	}
	values := url.Values{"lang": []string{h.localeFor(r)}}
	http.Redirect(w, r, "/repositories/"+strconv.FormatInt(result.ID, 10)+"?"+values.Encode(), http.StatusSeeOther)
}

func (h *Handler) watchAccess(r *http.Request) (allowed, requiresToken bool) {
	if h.watcher == nil {
		return false, false
	}
	if h.auth != nil {
		return h.isAdmin(r), false
	}
	if h.allowLocalWrites && watchLoopbackRequest(r) {
		return true, false
	}
	if len(h.writeToken) >= 24 && watchHTTPS(r) {
		return true, true
	}
	return false, false
}

func watchHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func watchLoopbackRequest(r *http.Request) bool {
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP"} {
		if r.Header.Get(header) != "" {
			return false
		}
	}
	host := r.Host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(remote)
	return ip != nil && ip.IsLoopback()
}

func watchSameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	scheme := "http"
	if watchHTTPS(r) {
		scheme = "https"
	}
	raw := r.Header.Get("Origin")
	if raw == "" {
		raw = r.Header.Get("Referer")
	}
	if raw == "" {
		return false
	}
	origin, err := url.Parse(raw)
	return err == nil && origin.User == nil && origin.Scheme == scheme && strings.EqualFold(origin.Host, r.Host)
}

func watchTokenEqual(expected, supplied string) bool {
	a, b := sha256.Sum256([]byte(expected)), sha256.Sum256([]byte(supplied))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

func (h *Handler) issueWatchNonce(w http.ResponseWriter, r *http.Request) (string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(random[:])
	h.watchMu.Lock()
	now := h.now()
	for key, expiration := range h.watchNonces {
		if !expiration.After(now) {
			delete(h.watchNonces, key)
		}
	}
	// Bound unauthenticated form state without retaining credentials or input.
	if len(h.watchNonces) >= 512 {
		var oldestKey string
		var oldest time.Time
		for key, expiration := range h.watchNonces {
			if oldest.IsZero() || expiration.Before(oldest) {
				oldestKey, oldest = key, expiration
			}
		}
		delete(h.watchNonces, oldestKey)
	}
	h.watchNonces[token] = now.Add(30 * time.Minute)
	h.watchMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: watchCookie, Value: token, Path: "/watch", MaxAge: 1800, HttpOnly: true, Secure: watchHTTPS(r), SameSite: http.SameSiteStrictMode})
	return token, nil
}

func (h *Handler) consumeWatchNonce(r *http.Request, token string) bool {
	cookie, err := r.Cookie(watchCookie)
	if err != nil || len(token) != 64 || !watchTokenEqual(cookie.Value, token) {
		return false
	}
	h.watchMu.Lock()
	defer h.watchMu.Unlock()
	expiration, exists := h.watchNonces[token]
	if !exists || !expiration.After(h.now()) {
		return false
	}
	delete(h.watchNonces, token)
	return true
}

func watchError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrImportQueueBusy):
		return http.StatusTooManyRequests, "busy"
	case errors.Is(err, ErrWatchInvalid):
		return http.StatusBadRequest, "invalid"
	case errors.Is(err, ErrWatchPrivate):
		return http.StatusNotFound, "private"
	case errors.Is(err, ErrWatchRateLimited):
		return http.StatusTooManyRequests, "rate_limited"
	case errors.Is(err, ErrWatchTopic):
		return http.StatusBadRequest, "topic_error"
	case errors.Is(err, ErrWatchIdentity):
		return http.StatusConflict, "identity_error"
	default:
		return http.StatusBadGateway, "unavailable"
	}
}

func (h *Handler) renderWatch(w http.ResponseWriter, r *http.Request, status int, input WatchRequest, errorKey string) {
	locale := h.localeFor(r)
	allowed, requiresToken := h.watchAccess(r)
	view := watchPageView{pageView: pageView{Meta: h.metaText(h.localizerFor(r), watchText(locale, "title"), watchText(locale, "description"), "watch", nil)}, Watch: watchFormData{Input: input, CanWrite: allowed, RequiresToken: requiresToken}}
	view.Meta.Locale, view.Meta.EnglishURL, view.Meta.ChineseURL = locale, languageURL(r, localeEnglish), languageURL(r, localeChinese)
	view.Meta.Auth = h.authInfo(r)
	if errorKey != "" {
		view.Watch.Error = watchText(locale, errorKey)
	}
	if allowed {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		topics, err := h.watcher.WatchTopics(ctx)
		if err != nil {
			view.Watch.CanWrite = false
			view.Watch.Error = watchText(locale, "unavailable")
			status = http.StatusServiceUnavailable
		} else {
			view.Watch.Topics = topics
			view.Watch.CSRFToken, err = h.issueWatchNonce(w, r)
			if err != nil {
				http.Error(w, "form unavailable", http.StatusInternalServerError)
				return
			}
		}
	} else if h.watcher != nil && len(h.writeToken) >= 24 && !watchHTTPS(r) {
		view.Watch.Error = watchText(locale, "https")
	}
	var output bytes.Buffer
	if err := h.templates[locale]["watch"].ExecuteTemplate(&output, "base", view); err != nil {
		h.logger.ErrorContext(r.Context(), "watch template failed", "error", err)
		http.Error(w, "form unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Language", locale)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = output.WriteTo(w)
}
