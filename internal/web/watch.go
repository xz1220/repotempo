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

const watchCookie = "repotempo_watch_binding"

func (h *Handler) watchForm(w http.ResponseWriter, r *http.Request) {
	input := WatchRequest{Repository: r.URL.Query().Get("repository"), Focus: r.URL.Query().Get("focus") == "1"}
	if len(input.Repository) > 300 {
		input.Repository = ""
	}
	if reader, ok := h.watcher.(WatchStateReader); ok && h.isSignedIn(r) && input.Repository != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		state, err := reader.WatchState(ctx, input.Repository)
		cancel()
		if err == nil {
			if h.isAdmin(r) && r.URL.Query().Get("import") == "1" {
				state.EditRepository = ""
			}
			if r.URL.Query().Has("focus") {
				state.Focus = input.Focus
			}
			input = state
		} else if !errors.Is(err, ErrWatchAdminRequired) && !errors.Is(err, ErrWatchInvalid) {
			h.renderWatch(w, r, http.StatusServiceUnavailable, input, "unavailable")
			return
		}
	}
	h.renderWatch(w, r, http.StatusOK, input, "")
}

func (h *Handler) watchAdd(w http.ResponseWriter, r *http.Request) {
	if !watchSameOrigin(r) {
		h.renderWatch(w, r, http.StatusForbidden, WatchRequest{}, "forbidden")
		return
	}
	writable, requiresToken := h.watchAccess(r)
	if h.watcher == nil || !writable {
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
	input := WatchRequest{Repository: strings.TrimSpace(r.PostForm.Get("repository")), EditRepository: strings.TrimSpace(r.PostForm.Get("edit_repository")), TopicSlug: strings.TrimSpace(r.PostForm.Get("topic")), Note: strings.TrimSpace(r.PostForm.Get("note")), Focus: r.PostForm.Get("focus") == "1"}
	if len(r.PostForm["edit_repository"]) > 1 {
		h.renderWatch(w, r, http.StatusBadRequest, input, "invalid")
		return
	}
	if values := r.PostForm["focus"]; len(values) > 1 || (len(values) == 1 && values[0] != "0" && values[0] != "1") {
		h.renderWatch(w, r, http.StatusBadRequest, input, "invalid")
		return
	}
	if len(input.Repository) > 300 || len(input.EditRepository) > 300 || len(input.TopicSlug) > 100 || utf8.RuneCountInString(input.Note) > 2000 {
		h.renderWatch(w, r, http.StatusBadRequest, WatchRequest{}, "invalid")
		return
	}
	if requiresToken && !watchTokenEqual(h.writeToken, r.PostForm.Get("operator_token")) {
		h.renderWatch(w, r, http.StatusForbidden, input, "forbidden")
		return
	}
	if len(r.PostForm["csrf_token"]) != 1 || !h.consumeWatchNonce(r, r.PostForm.Get("csrf_token")) {
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
	if importer, ok := h.watcher.(Importer); ok && input.EditRepository == "" && (h.auth == nil || h.isAdmin(r)) {
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
	if h.watcher == nil && h.focusUpdater == nil {
		return false, false
	}
	if h.auth != nil {
		return h.isSignedIn(r), false
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
	// A stable browser binding is visible on library GETs as well as /watch.
	// The form nonce remains unique and independently consumable for each page.
	binding, validBinding := watchBrowserBinding(r)
	if !validBinding {
		binding = token
	}
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
	h.watchNonces[h.watchNonceKey(r, token, binding)] = now.Add(30 * time.Minute)
	h.watchMu.Unlock()
	// Use a new name so a legacy /watch cookie cannot shadow this root binding.
	http.SetCookie(w, &http.Cookie{Name: watchCookie, Value: binding, Path: "/", MaxAge: 1800, HttpOnly: true, Secure: watchHTTPS(r), SameSite: http.SameSiteStrictMode})
	return token, nil
}

func watchBrowserBinding(r *http.Request) (string, bool) {
	cookieValue, cookieCount := "", 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == watchCookie {
			cookieValue = cookie.Value
			cookieCount++
		}
	}
	if cookieCount != 1 || len(cookieValue) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(cookieValue); err != nil {
		return "", false
	}
	return cookieValue, true
}

func (h *Handler) consumeWatchNonce(r *http.Request, token string) bool {
	if len(token) != 64 {
		return false
	}
	binding, validBinding := watchBrowserBinding(r)
	if h.auth != nil {
		// Authenticated CSRF uses a synchronizer token bound below to the
		// exact login session. Another page's cookie cannot invalidate it.
		if currentAuth(r).Session == nil {
			return false
		}
	} else if !validBinding {
		return false
	}
	h.watchMu.Lock()
	defer h.watchMu.Unlock()
	key := h.watchNonceKey(r, token, binding)
	expiration, exists := h.watchNonces[key]
	if !exists || !expiration.After(h.now()) {
		return false
	}
	delete(h.watchNonces, key)
	return true
}

func (h *Handler) watchNonceKey(r *http.Request, token, binding string) string {
	if h.auth != nil {
		if session := currentAuth(r).Session; session != nil {
			return token + ":" + strconv.FormatInt(session.GitHubUserID, 10) + ":" + session.CSRFToken
		}
		return token + ":anonymous"
	}
	return token + ":" + binding
}

func watchError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrWatchLimit):
		return http.StatusUnprocessableEntity, "personal_limit"
	case errors.Is(err, ErrWatchAdminRequired):
		return http.StatusForbidden, "admin_required"
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
	allowed = allowed && h.watcher != nil
	view := watchPageView{pageView: pageView{Meta: h.metaText(h.localizerFor(r), watchText(locale, "title"), watchText(locale, "description"), "watch", nil)}, Watch: watchFormData{Input: input, CanWrite: allowed, RequiresToken: requiresToken}}
	view.Meta.Locale, view.Meta.EnglishURL, view.Meta.ChineseURL = locale, languageURL(r, localeEnglish), languageURL(r, localeChinese)
	view.Meta.Auth = h.authInfo(r)
	view.Watch.PersonalOnly = h.auth != nil && (!h.isAdmin(r) || input.EditRepository != "")
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
