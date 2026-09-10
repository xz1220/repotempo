package web

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"time"
)

const focusFormMaxBytes = 8 * 1024

var (
	ErrFocusInvalid     = errors.New("focus: invalid request")
	ErrFocusNotFound    = errors.New("focus: repository not found")
	ErrFocusUnavailable = errors.New("focus: update unavailable")
)

// FocusUpdater persists an explicit watchlist choice. It is optional so a
// read-only Web handler does not need to expose repository mutations.
type FocusUpdater interface {
	SetRepositoryFocus(context.Context, int64, bool) error
}

func (h *Handler) watchFocus(w http.ResponseWriter, r *http.Request) {
	if !watchSameOrigin(r) {
		h.focusResponse(w, r, http.StatusForbidden, "forbidden")
		return
	}
	writable, requiresToken := h.watchAccess(r)
	if !writable {
		h.focusResponse(w, r, http.StatusForbidden, "forbidden")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		h.focusResponse(w, r, http.StatusUnsupportedMediaType, "unsupported")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, focusFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.focusResponse(w, r, http.StatusRequestEntityTooLarge, "too_large")
			return
		}
		h.focusResponse(w, r, http.StatusBadRequest, "invalid")
		return
	}

	repositoryID, ok := exactPositiveID(r.PostForm["repository_id"])
	if !ok {
		h.focusResponse(w, r, http.StatusBadRequest, "invalid")
		return
	}
	focus, ok := exactFocusValue(r.PostForm["focus"])
	if !ok {
		h.focusResponse(w, r, http.StatusBadRequest, "invalid")
		return
	}
	returnTo, ok := focusReturnPath(r.PostForm["return_to"])
	if !ok {
		h.focusResponse(w, r, http.StatusBadRequest, "invalid")
		return
	}
	operatorTokens := r.PostForm["operator_token"]
	if len(operatorTokens) > 1 {
		h.focusResponse(w, r, http.StatusBadRequest, "invalid")
		return
	}
	if requiresToken && (len(operatorTokens) != 1 || !watchTokenEqual(h.writeToken, operatorTokens[0])) {
		h.focusResponse(w, r, http.StatusForbidden, "forbidden")
		return
	}
	csrfTokens := r.PostForm["csrf_token"]
	if len(csrfTokens) != 1 || !h.consumeWatchNonce(r, csrfTokens[0]) {
		h.focusResponse(w, r, http.StatusConflict, "csrf")
		return
	}
	if h.focusUpdater == nil {
		h.focusResponse(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.focusUpdater.SetRepositoryFocus(ctx, repositoryID, focus); err != nil {
		switch {
		case errors.Is(err, ErrFocusInvalid):
			h.focusResponse(w, r, http.StatusBadRequest, "invalid")
		case errors.Is(err, ErrFocusNotFound):
			h.focusResponse(w, r, http.StatusNotFound, "not_found")
		default:
			h.focusResponse(w, r, http.StatusServiceUnavailable, "unavailable")
		}
		return
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

func exactPositiveID(values []string) (int64, bool) {
	if len(values) != 1 {
		return 0, false
	}
	value, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || value <= 0 || values[0] != strconv.FormatInt(value, 10) {
		return 0, false
	}
	return value, true
}

func exactFocusValue(values []string) (bool, bool) {
	if len(values) != 1 {
		return false, false
	}
	switch values[0] {
	case "0":
		return false, true
	case "1":
		return true, true
	default:
		return false, false
	}
}

func focusReturnPath(values []string) (string, bool) {
	if len(values) == 0 {
		return "/repositories", true
	}
	if len(values) != 1 {
		return "", false
	}
	return validatedLibraryReturnURL(values[0])
}

func (h *Handler) focusResponse(w http.ResponseWriter, r *http.Request, status int, key string) {
	locale := h.localeFor(r)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Language", locale)
	http.Error(w, focusText(locale, key), status)
}

func focusText(locale, key string) string {
	message, ok := focusMessages[key]
	if !ok {
		message = focusMessages["unavailable"]
	}
	if locale == localeChinese {
		return message[0]
	}
	return message[1]
}

var focusMessages = map[string][2]string{
	"invalid":     {"关注请求无效，请刷新项目库后重试。", "The watchlist request is invalid. Refresh the project library and try again."},
	"unsupported": {"只接受项目库提交的表单。", "Only project-library form submissions are accepted."},
	"too_large":   {"关注请求内容过长，请刷新项目库后重试。", "The watchlist request is too large. Refresh the project library and try again."},
	"forbidden":   {"未能验证本次关注请求，请重新登录或刷新页面。", "We could not verify this watchlist request. Sign in again or refresh the page."},
	"csrf":        {"表单已过期或已提交，请刷新项目库后重试。", "This form has expired or was already submitted. Refresh the project library and try again."},
	"not_found":   {"这个项目已不存在，请刷新项目库。", "This repository no longer exists. Refresh the project library."},
	"unavailable": {"暂时无法更新关注状态，请稍后重试。", "The watchlist could not be updated right now. Please try again later."},
}
