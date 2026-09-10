package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/agentaccess"
)

type agentAccessPageView struct {
	pageView
	Keys      []domain.AgentKey
	Created   *agentaccess.CreatedKey
	CSRFToken string
	Endpoint  string
	Error     string
}

func (h *Handler) agentAccountSession(w http.ResponseWriter, r *http.Request) *domain.AuthSession {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if h.auth == nil || h.agentAccess == nil {
		h.agentAccountError(w, r, "Agent access is unavailable", http.StatusServiceUnavailable)
		return nil
	}
	if session := currentAuth(r).Session; session != nil && h.canonicalAuthRequest(r) {
		return session
	}
	if r.Method == http.MethodGet {
		http.Redirect(w, r, h.loginURL(r, "/account/api"), http.StatusSeeOther)
	} else {
		h.agentAccountError(w, r, "Sign in first", http.StatusUnauthorized)
	}
	return nil
}

func (h *Handler) accountAPI(w http.ResponseWriter, r *http.Request) {
	if session := h.agentAccountSession(w, r); session != nil {
		h.renderAgentAccount(w, r, session, nil, "", http.StatusOK)
	}
}

// Account forms have one-time, server-side nonces bound to the authenticated
// session's CSRF secret. A second submit cannot create another unseen secret.
func (h *Handler) agentFormNonce(session *domain.AuthSession) (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(random)
	h.watchMu.Lock()
	defer h.watchMu.Unlock()
	for key, until := range h.watchNonces {
		if !until.After(h.now()) {
			delete(h.watchNonces, key)
		}
	}
	if len(h.watchNonces) >= 512 {
		return "", agentaccess.ErrUnavailable
	}
	h.watchNonces["agent:"+session.CSRFToken+":"+nonce] = h.now().Add(30 * time.Minute)
	return nonce, nil
}

func (h *Handler) consumeAgentNonce(session *domain.AuthSession, nonce string) bool {
	if len(nonce) != 64 {
		return false
	}
	key := "agent:" + session.CSRFToken + ":" + nonce
	h.watchMu.Lock()
	defer h.watchMu.Unlock()
	until, ok := h.watchNonces[key]
	delete(h.watchNonces, key)
	return ok && until.After(h.now())
}

func (h *Handler) agentAccountForm(w http.ResponseWriter, r *http.Request, session *domain.AuthSession) bool {
	if !watchSameOrigin(r) {
		h.agentAccountError(w, r, "Invalid origin", http.StatusForbidden)
		return false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		h.agentAccountError(w, r, "Expected a form", http.StatusUnsupportedMediaType)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	if err := r.ParseForm(); err != nil {
		h.agentAccountError(w, r, "Invalid form", http.StatusBadRequest)
		return false
	}
	if len(r.PostForm["csrf_token"]) != 1 || !h.consumeAgentNonce(session, r.PostForm.Get("csrf_token")) {
		h.agentAccountError(w, r, "Form expired or already submitted; refresh the account page", http.StatusForbidden)
		return false
	}
	return true
}

func (h *Handler) accountAPIKeyCreate(w http.ResponseWriter, r *http.Request) {
	session := h.agentAccountSession(w, r)
	if session == nil || !h.agentAccountForm(w, r, session) {
		return
	}
	if len(r.PostForm["name"]) != 1 || len(r.PostForm["expires_days"]) > 1 || len(r.PostForm["scope"]) < 1 || len(r.PostForm["scope"]) > 2 {
		h.renderAgentAccount(w, r, session, nil, "Invalid key settings", http.StatusBadRequest)
		return
	}
	days := 90
	if r.PostForm.Has("expires_days") {
		var err error
		days, err = strconv.Atoi(r.PostForm.Get("expires_days"))
		if err != nil {
			h.renderAgentAccount(w, r, session, nil, "Invalid key expiry", http.StatusBadRequest)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	created, err := h.agentAccess.Create(ctx, session.GitHubUserID, r.PostForm.Get("name"), r.PostForm["scope"], days)
	if err != nil {
		status, message := http.StatusServiceUnavailable, "Could not create the key; try again later"
		if errors.Is(err, agentaccess.ErrInvalid) {
			status, message = http.StatusBadRequest, "Choose a name, read scopes, and an expiry of 30, 90, or 365 days"
		}
		if errors.Is(err, domain.ErrAgentKeyCapacity) {
			status, message = http.StatusTooManyRequests, "Key limit reached; revoke unused keys or try again later"
		}
		h.renderAgentAccount(w, r, session, nil, message, status)
		return
	}
	h.renderAgentAccount(w, r, session, &created, "", http.StatusCreated)
}

func (h *Handler) accountAPIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	session := h.agentAccountSession(w, r)
	if session == nil || !h.agentAccountForm(w, r, session) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.agentAccess.Revoke(ctx, session.GitHubUserID, r.PathValue("id")); err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, agentaccess.ErrInvalid) {
			status = http.StatusNotFound
		}
		h.renderAgentAccount(w, r, session, nil, "Could not revoke this key", status)
		return
	}
	http.Redirect(w, r, queryPath("/account/api", url.Values{"lang": {h.localeFor(r)}}), http.StatusSeeOther)
}

func (h *Handler) renderAgentAccount(w http.ResponseWriter, r *http.Request, session *domain.AuthSession, created *agentaccess.CreatedKey, message string, status int) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	keys, err := h.agentAccess.List(ctx, session.GitHubUserID)
	if err != nil {
		h.agentAccountError(w, r, "Agent access is unavailable", http.StatusServiceUnavailable)
		return
	}
	nonce, err := h.agentFormNonce(session)
	if err != nil {
		h.agentAccountError(w, r, "Refresh this page in a moment", http.StatusServiceUnavailable)
		return
	}
	view := agentAccessPageView{Keys: keys, Created: created, CSRFToken: nonce, Endpoint: h.auth.PublicURL(), Error: h.agentAccountMessage(r, message)}
	view.Meta = h.metaText(h.localizerFor(r), "API / Agent", "Connect your agents to RepoTempo", "account", nil)
	view.Meta.Locale = h.localeFor(r)
	view.Meta.Auth = h.authInfo(r)
	view.Meta.ChineseURL = "/account/api?lang=zh-CN"
	view.Meta.EnglishURL = "/account/api?lang=en"
	var output bytes.Buffer
	tmpl := h.templates[view.Meta.Locale]["account_api"]
	if tmpl == nil {
		h.agentAccountError(w, r, "Agent access is unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := tmpl.ExecuteTemplate(&output, "base", view); err != nil {
		h.agentAccountError(w, r, "Agent access is unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", view.Meta.Locale)
	w.WriteHeader(status)
	_, _ = output.WriteTo(w)
}

func (h *Handler) agentAccountError(w http.ResponseWriter, r *http.Request, message string, status int) {
	http.Error(w, h.agentAccountMessage(r, message), status)
}
func (h *Handler) agentAccountMessage(r *http.Request, message string) string {
	if h.localeFor(r) != localeChinese {
		return message
	}
	translations := map[string]string{
		"Agent access is unavailable": "Agent 接入暂时不可用，请稍后重试。",
		"Sign in first":               "请先登录。",
		"Invalid origin":              "未能验证请求来源，请刷新账户页面后重试。",
		"Expected a form":             "请使用账户页面中的表单。",
		"Invalid form":                "表单无效，请刷新后重试。",
		"Form expired or already submitted; refresh the account page": "表单已过期或已提交，请刷新账户页面。",
		"Invalid key settings":                                             "请填写密钥名称，并至少选择一项读取权限。",
		"Invalid key expiry":                                               "请选择有效的密钥期限。",
		"Could not create the key; try again later":                        "密钥创建失败，请稍后重试。",
		"Choose a name, read scopes, and an expiry of 30, 90, or 365 days": "请填写名称、选择读取权限和 30、90 或 365 天的有效期。",
		"Key limit reached; revoke unused keys or try again later":         "已达到密钥数量或创建频率上限，请撤销不用的密钥或稍后再试。",
		"Could not revoke this key":                                        "未能撤销这把密钥，请刷新页面确认状态。",
		"Refresh this page in a moment":                                    "请稍后刷新此页面。",
	}
	if translated, ok := translations[message]; ok {
		return translated
	}
	return message
}
