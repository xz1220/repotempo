package web

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	loginauth "github.com/xz1220/repotempo/internal/service/auth"
)

type Authenticator interface {
	PublicURL() string
	SecureCookies() bool
	Begin(context.Context, string) (loginauth.LoginStart, error)
	Complete(context.Context, string, string, string) (loginauth.LoginResult, error)
	Session(context.Context, string) (domain.AuthSession, error)
	Logout(context.Context, string, string) error
	Revoke(context.Context, string) error
}

type authContextKey struct{}
type requestAuth struct {
	Session     *domain.AuthSession
	Unavailable bool
}
type authView struct {
	Enabled, SignedIn          bool
	Admin                      bool
	UserID                     int64
	PublicSignup               bool
	Login, LoginURL, CSRFToken string
}
type loginPageView struct {
	pageView
	StartURL, Error string
	Configured      bool
}

func (h *Handler) sessionCookieName() string {
	if h.auth.SecureCookies() {
		return "__Host-repotempo_session"
	}
	return "repotempo_session"
}
func (h *Handler) bindingCookieName() string {
	if h.auth.SecureCookies() {
		return "__Host-repotempo_oauth"
	}
	return "repotempo_oauth"
}
func (h *Handler) setAuthCookie(w http.ResponseWriter, name, value string, expires time.Time) {
	age := int(expires.Sub(h.now()).Seconds())
	if value == "" {
		age = -1
		expires = time.Unix(1, 0)
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, MaxAge: age, HttpOnly: true, Secure: h.auth.SecureCookies(), SameSite: http.SameSiteLaxMode})
}
func uniqueCookie(r *http.Request, name string) string {
	value, count := "", 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			value = cookie.Value
			count++
		}
	}
	if count != 1 || len(value) != 64 {
		return ""
	}
	return value
}

func (h *Handler) canonicalAuthRequest(r *http.Request) bool {
	if h.auth == nil {
		return false
	}
	origin, err := url.Parse(h.auth.PublicURL())
	if err != nil || !strings.EqualFold(origin.Host, r.Host) {
		return false
	}
	if origin.Scheme == "http" {
		return watchLoopbackRequest(r)
	}
	if r.TLS != nil {
		return true
	}
	// Only the local reverse proxy may assert the external TLS scheme.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https"
}

func currentAuth(r *http.Request) requestAuth {
	value, _ := r.Context().Value(authContextKey{}).(requestAuth)
	return value
}
func (h *Handler) isAdmin(r *http.Request) bool {
	return h.isSignedIn(r) && currentAuth(r).Session.Admin
}
func (h *Handler) isSignedIn(r *http.Request) bool {
	return h.auth != nil && currentAuth(r).Session != nil
}

func (h *Handler) authenticateRequest(w http.ResponseWriter, r *http.Request) *http.Request {
	if h.auth == nil || strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/readyz" || r.URL.Path == "/healthz" {
		return r
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Cookie")
	state := requestAuth{}
	if h.canonicalAuthRequest(r) {
		if token := uniqueCookie(r, h.sessionCookieName()); token != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			session, err := h.auth.Session(ctx, token)
			cancel()
			if err == nil && session.GitHubUserID > 0 && session.Login != "" {
				state.Session = &session
			} else if errors.Is(err, loginauth.ErrUnauthenticated) || errors.Is(err, loginauth.ErrForbidden) {
				h.setAuthCookie(w, h.sessionCookieName(), "", time.Time{})
			} else {
				state.Unavailable = true
			}
		}
	}
	principal := domain.Principal{}
	if state.Session != nil {
		principal = domain.Principal{UserID: state.Session.GitHubUserID, Login: state.Session.Login, Admin: state.Session.Admin}
	}
	return r.WithContext(domain.WithPrincipal(context.WithValue(r.Context(), authContextKey{}, state), principal))
}

func ownerRoute(r *http.Request) bool {
	path := r.URL.Path
	if path == "/watch" || strings.HasPrefix(path, "/watch/") || path == "/runs" {
		return true
	}
	// Check all occurrences; never allow a duplicate-key form to bypass scope.
	for _, value := range r.URL.Query()["focus"] {
		if value == "1" {
			return true
		}
	}
	for _, value := range r.URL.Query()["view"] {
		if value == "focus" {
			return true
		}
	}
	return false
}
func authReturnPath(raw string) string {
	value, err := loginauth.SafeReturnPath(raw)
	if err != nil {
		return "/repositories"
	}
	return value
}
func (h *Handler) loginURL(r *http.Request, returnPath string) string {
	values := url.Values{"next": {authReturnPath(returnPath)}, "lang": {h.localeFor(r)}}
	return h.auth.PublicURL() + "/auth/login?" + values.Encode()
}
func (h *Handler) authorizeRequest(w http.ResponseWriter, r *http.Request) bool {
	adminOnly := r.URL.Path == "/runs" || strings.HasPrefix(r.URL.Path, "/watch/imports/")
	if h.auth == nil || !ownerRoute(r) || h.isAdmin(r) || h.isSignedIn(r) && !adminOnly {
		return true
	}
	if h.isSignedIn(r) {
		http.Error(w, authText(h.localeFor(r), "admin_required"), http.StatusForbidden)
		return false
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.Redirect(w, r, h.loginURL(r, r.URL.RequestURI()), http.StatusSeeOther)
			return false
		}
	}
	status := http.StatusUnauthorized
	if currentAuth(r).Unavailable {
		status = http.StatusServiceUnavailable
	}
	http.Error(w, authText(h.localeFor(r), "required"), status)
	return false
}
func (h *Handler) authInfo(r *http.Request) authView {
	if h.auth == nil {
		return authView{}
	}
	view := authView{Enabled: true, LoginURL: h.loginURL(r, r.URL.RequestURI())}
	if config, ok := h.auth.(interface{ PublicSignupEnabled() bool }); ok {
		view.PublicSignup = config.PublicSignupEnabled()
	}
	if session := currentAuth(r).Session; session != nil {
		view.SignedIn = true
		view.Admin = session.Admin
		view.UserID = session.GitHubUserID
		view.Login = session.Login
		view.CSRFToken = session.CSRFToken
	}
	return view
}

func (h *Handler) authLogin(w http.ResponseWriter, r *http.Request) {
	locale := h.localeFor(r)
	values, parseErr := url.ParseQuery(r.URL.RawQuery)
	next := "/repositories"
	if parseErr == nil && len(values["next"]) <= 1 {
		next = authReturnPath(values.Get("next"))
	}
	view := loginPageView{pageView: pageView{Meta: h.metaText(h.localizerFor(r), authText(locale, "title"), authText(locale, "description"), "auth", nil)}, Configured: h.auth != nil}
	view.Meta.Locale = locale
	view.Meta.Auth = h.authInfo(r)
	// Language links never carry an OAuth code, state or arbitrary errors.
	for _, language := range []string{localeChinese, localeEnglish} {
		link := queryPath("/auth/login", url.Values{"next": {next}, "lang": {language}})
		if language == localeChinese {
			view.Meta.ChineseURL = link
		} else {
			view.Meta.EnglishURL = link
		}
	}
	status := http.StatusOK
	if h.auth == nil {
		view.Error = authText(locale, "unconfigured")
		status = http.StatusServiceUnavailable
	} else {
		view.StartURL = h.auth.PublicURL() + queryPath("/auth/github/start", url.Values{"next": {next}, "lang": {locale}})
		if errorKey := values.Get("error"); errorKey != "" {
			switch errorKey {
			case "invalid", "forbidden", "unavailable", "limited":
			default:
				errorKey = "invalid"
			}
			view.Error = authText(locale, errorKey)
		}
	}
	var output bytes.Buffer
	if err := h.templates[locale]["login"].ExecuteTemplate(&output, "base", view); err != nil {
		http.Error(w, "login unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = output.WriteTo(w)
}

func (h *Handler) authStart(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		h.authLogin(w, r)
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values["next"]) > 1 {
		h.authError(w, r, "invalid")
		return
	}
	next := authReturnPath(values.Get("next"))
	if !h.canonicalAuthRequest(r) {
		http.Redirect(w, r, h.auth.PublicURL()+queryPath("/auth/github/start", url.Values{"next": {next}, "lang": {h.localeFor(r)}}), http.StatusSeeOther)
		return
	}
	if !h.authStarts.allow(r, h.now()) {
		w.Header().Set("Retry-After", "600")
		http.Error(w, authText(h.localeFor(r), "limited"), http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := h.auth.Begin(ctx, next)
	if err != nil {
		h.authError(w, r, "unavailable")
		return
	}
	h.setAuthCookie(w, h.bindingCookieName(), result.BrowserBinding, h.now().Add(loginauth.LoginLifetime))
	http.Redirect(w, r, result.AuthorizationURL, http.StatusSeeOther)
}

func (h *Handler) authCallback(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.NotFound(w, r)
		return
	}
	if !h.canonicalAuthRequest(r) {
		http.Error(w, authText(h.localeFor(r), "invalid"), http.StatusBadRequest)
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	binding := uniqueCookie(r, h.bindingCookieName())
	h.setAuthCookie(w, h.bindingCookieName(), "", time.Time{})
	if err != nil || len(values["state"]) != 1 || len(values["code"]) != 1 || len(values["error"]) > 0 || binding == "" {
		h.authError(w, r, "invalid")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := h.auth.Complete(ctx, values.Get("state"), values.Get("code"), binding)
	if err != nil {
		key := "invalid"
		if errors.Is(err, loginauth.ErrForbidden) {
			key = "forbidden"
		} else if errors.Is(err, loginauth.ErrProvider) || errors.Is(err, loginauth.ErrStorage) {
			key = "unavailable"
		}
		h.authError(w, r, key)
		return
	}
	if old := uniqueCookie(r, h.sessionCookieName()); old != "" {
		if err := h.auth.Revoke(ctx, old); err != nil {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer stop()
			_ = h.auth.Revoke(cleanup, result.Token)
			h.authError(w, r, "unavailable")
			return
		}
	}
	h.setAuthCookie(w, h.sessionCookieName(), result.Token, result.Session.ExpiresAt)
	http.Redirect(w, r, authReturnPath(result.ReturnPath), http.StatusSeeOther)
}

func (h *Handler) authLogout(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.NotFound(w, r)
		return
	}
	if !h.canonicalAuthRequest(r) || !watchSameOrigin(r) || !h.isSignedIn(r) {
		http.Error(w, authText(h.localeFor(r), "required"), http.StatusForbidden)
		return
	}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/x-www-form-urlencoded" {
		http.Error(w, "unsupported form", 415)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || len(r.PostForm["csrf_token"]) != 1 {
		http.Error(w, "invalid form", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.auth.Logout(ctx, uniqueCookie(r, h.sessionCookieName()), r.PostForm.Get("csrf_token")); err != nil {
		http.Error(w, authText(h.localeFor(r), "invalid"), http.StatusForbidden)
		return
	}
	h.setAuthCookie(w, h.sessionCookieName(), "", time.Time{})
	http.Redirect(w, r, queryPath("/repositories", url.Values{"lang": {h.localeFor(r)}}), http.StatusSeeOther)
}
func (h *Handler) authError(w http.ResponseWriter, r *http.Request, key string) {
	http.Redirect(w, r, queryPath("/auth/login", url.Values{"error": {key}, "lang": {h.localeFor(r)}}), http.StatusSeeOther)
}

func authText(locale, key string) string {
	pair, ok := authMessages[key]
	if !ok {
		pair = authMessages["invalid"]
	}
	if locale == localeChinese {
		return pair[0]
	}
	return pair[1]
}

var authMessages = map[string][2]string{
	"title":          {"登录 RepoTempo", "Sign in to RepoTempo"},
	"description":    {"使用 GitHub 登录，保存自己的关注与项目备注，并连接 AI Agent。", "Sign in with GitHub to save your own watchlist and notes and connect an AI agent."},
	"login":          {"GitHub 登录", "Sign in with GitHub"},
	"logout":         {"退出登录", "Sign out"},
	"scope":          {"仅验证 GitHub 身份，不申请私有仓库、邮箱或代码读写权限。", "We verify your GitHub identity only; no private repository, email or code-write scopes are requested."},
	"workspace":      {"关注与备注归当前 GitHub 账号所有，其他用户不可见。已读标记仍保存在本浏览器。", "Your watchlist and notes belong to your GitHub account and are private. Reading marks remain in this browser."},
	"required":       {"请先使用 GitHub 账号登录。", "Sign in with GitHub first."},
	"admin_required": {"此操作仅对站点管理员开放。", "This operation is restricted to site administrators."},
	"invalid":        {"登录请求已失效、被取消或未通过验证，请重新登录。", "The sign-in request expired, was cancelled or could not be verified. Please sign in again."},
	"forbidden":      {"这个 GitHub 账号不在管理员名单中，未授予管理权限。", "This GitHub account is not on the administrator allowlist. No management access was granted."},
	"unavailable":    {"暂时无法验证 GitHub 登录，请稍后重试。", "GitHub sign-in could not be verified right now. Please try again later."},
	"limited":        {"登录尝试过于频繁，请稍后再试。", "Too many sign-in attempts. Please try again later."},
	"unconfigured":   {"GitHub 登录尚未配置，需先完成 OAuth 应用配置。公开项目仍可正常浏览。", "GitHub sign-in is not configured. Complete the OAuth application settings first; public projects remain available."},
	"back":           {"继续浏览公开项目", "Browse public projects"},
	"signed_in":      {"已登录，你的关注与备注将保存到当前账号。", "Signed in. Your watchlist and notes are saved to your account."},
}
