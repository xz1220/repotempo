package web

import "strings"

func projectGitHubURL(fullName, raw string) string {
	parts := strings.Split(fullName, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" && parts[1] != "." && parts[1] != ".." {
		valid := true
		for i, part := range parts {
			for _, char := range part {
				if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || i == 1 && (char == '_' || char == '.')) {
					valid = false
				}
			}
		}
		if valid {
			return "https://github.com/" + fullName
		}
	}
	return githubURL(raw)
}

func briefText(value string) string {
	text := []rune(strings.Join(strings.Fields(value), " "))
	if len(text) > 280 {
		return string(text[:280]) + "…"
	}
	return string(text)
}

func aiAnalysis(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	for _, prefix := range []string{"codex", "kimi", "claude", "gemini", "openai", "ai_", "ai-"} {
		if strings.HasPrefix(source, prefix) {
			return true
		}
	}
	return false
}

func analysisSource(source string) string {
	for _, name := range []string{"Codex", "Kimi", "Claude", "Gemini", "OpenAI"} {
		if strings.HasPrefix(strings.ToLower(source), strings.ToLower(name)) {
			return name
		}
	}
	return source
}

func init() {
	pairs := map[string][2]string{
		"feed.list_label":         {"项目阅读列表", "Project reading feed"},
		"feed.brief":              {"项目简读", "Project brief"},
		"feed.ai_brief":           {"AI 简读", "AI reading brief"},
		"feed.saved_brief":        {"已保存的项目说明", "Saved project note"},
		"feed.legacy_source":      {"历史日报研究", "Archived daily research"},
		"feed.legacy_date":        {"原解读时间未记录", "Original research date not recorded"},
		"feed.not_read":           {"尚未解读", "Not reviewed yet"},
		"feed.pending_help":       {"尚未保存阅读摘要，可先查看项目介绍或在 GitHub 中阅读。", "No reading summary has been saved. Start with the description or read the project on GitHub."},
		"feed.reading_source":     {"阅读来源：%s", "Reading by %s"},
		"feed.previous_gain":      {"上期增长 %s", "Previous gain %s"},
		"feed.sample_rank":        {"可比样本排名", "Comparable-sample rank"},
		"feed.no_comparison":      {"缺少所选周期的可比观测", "Not enough observations for this period"},
		"feed.comparison_status":  {"对比状态", "Comparison status"},
		"feed.added":              {"收录于", "Added"},
		"feed.created":            {"创建于", "Created"},
		"feed.origin":             {"首次发现", "First found via"},
		"feed.details":            {"查看详情", "View details"},
		"feed.mark_read":          {"标记已读", "Mark as read"},
		"feed.mark_unread":        {"已读 · 标记未读", "Read · mark as unread"},
		"feed.reading_local_note": {"已读标记仅保存在当前浏览器和站点，不跨设备同步", "Read marks stay in this browser and site; they do not sync across devices."},
		"feed.reading_load_error": {"无法读取本浏览器的阅读状态。", "Reading status could not be loaded from this browser."},
		"feed.reading_save_error": {"未能保存阅读状态，请重试。", "Reading status was not saved. Please try again."},
	}
	for key, pair := range pairs {
		messageCatalog[localeChinese][key] = pair[0]
		messageCatalog[localeEnglish][key] = pair[1]
	}
}
