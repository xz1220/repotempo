package web

import (
	"context"
	"errors"
)

var (
	ErrWatchInvalid     = errors.New("watch: invalid repository input")
	ErrWatchPrivate     = errors.New("watch: repository is private or unavailable")
	ErrWatchRateLimited = errors.New("watch: GitHub rate limited")
	ErrWatchUnavailable = errors.New("watch: source unavailable")
	ErrWatchTopic       = errors.New("watch: topic is unavailable")
	ErrWatchIdentity    = errors.New("watch: repository identity conflict")
)

type WatchRequest struct {
	Repository string
	TopicSlug  string
	Note       string
}

type WatchResult struct {
	ID       int64
	FullName string
	Created  bool
}

type Watcher interface {
	AddWatch(context.Context, WatchRequest) (WatchResult, error)
	WatchTopics(context.Context) ([]TopicRef, error)
}

type watchPageView struct {
	pageView
	Watch watchFormData
}

type watchFormData struct {
	Input         WatchRequest
	Topics        []TopicRef
	CSRFToken     string
	CanWrite      bool
	RequiresToken bool
	Error         string
}

func watchText(locale, key string) string {
	copy, ok := watchMessages[key]
	if !ok {
		return key
	}
	if locale == localeChinese {
		return copy[0]
	}
	return copy[1]
}

var watchMessages = map[string][2]string{
	"title":           {"添加关注", "Track a project"},
	"description":     {"添加感兴趣的 GitHub 项目，从今天开始记录它的 Star 变化。", "Add a GitHub project and start recording its star history today."},
	"repository":      {"GitHub 项目", "GitHub repository"},
	"repository_hint": {"粘贴公开项目链接，或输入 owner/repository。", "Paste a public repository URL or enter owner/repository."},
	"topic":           {"项目类型（可选）", "Project type (optional)"},
	"topic_default":   {"自动识别 / 保留已有分类", "Detect automatically / keep existing types"},
	"note":            {"关注原因（可选）", "Why follow this project? (optional)"},
	"note_hint":       {"最多 2,000 字；已有备注会保留。", "Up to 2,000 characters. Existing notes are preserved."},
	"token":           {"管理口令", "Management token"},
	"token_hint":      {"使用站点管理员配置的管理口令。", "Use the management token configured by the site operator."},
	"submit":          {"添加并开始观测", "Add and track"},
	"cancel":          {"返回项目库", "Back to projects"},
	"expectation":     {"添加时会从 GitHub 获取项目简介和当前 Star，随后每天继续观测。重复添加不会丢失已有历史。", "We fetch its description and current stars from GitHub, then continue daily observations. Adding it again preserves its history."},
	"read_only":       {"当前站点仅供浏览。请在服务器本机打开此页面，或由管理员配置管理口令后添加项目。", "This site is currently read-only. Open it on the server itself, or ask the operator to configure a management token."},
	"https":           {"请通过 HTTPS 打开本站后使用管理口令。", "Open this site over HTTPS to use the management token."},
	"invalid":         {"请输入有效的公开 GitHub 项目链接或 owner/repository，并检查输入长度。", "Enter a valid public GitHub URL or owner/repository, and check the input lengths."},
	"private":         {"没有找到这个公开项目。请检查名称；私有项目暂不支持。", "We could not find this public repository. Check the name; private repositories are not supported."},
	"rate_limited":    {"GitHub 暂时限制了请求次数，请稍后重试。", "GitHub has temporarily limited requests. Please try again later."},
	"unavailable":     {"暂时无法获取项目数据，请稍后重试。", "Project data is temporarily unavailable. Please try again later."},
	"topic_error":     {"这个分类已不可用，请重新选择。", "This type is no longer available. Please choose another."},
	"identity_error":  {"这个名称与库中已有项目的 GitHub ID 不一致，请管理员核实后再添加。", "This name has a different GitHub ID from the stored project. Ask the operator to check its identity."},
	"csrf":            {"表单已过期或已提交，请重试。", "This form has expired or was already submitted. Please try again."},
	"forbidden":       {"未能验证本次添加请求，请检查管理口令或重新打开页面。", "We could not verify this request. Check the management token or reopen this page."},
	"busy":            {"正在添加项目，请稍后重试。", "A project is being added. Please try again shortly."},
	"too_large":       {"输入内容过长，请缩短项目地址或备注。", "The submission is too large. Shorten the repository address or note."},
}
