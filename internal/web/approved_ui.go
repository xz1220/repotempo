package web

import "strings"

func repositoryOwner(fullName string) string {
	owner, _, found := strings.Cut(fullName, "/")
	if !found || owner == "" {
		return fullName
	}
	return owner
}

func repositoryName(fullName string) string {
	_, name, found := strings.Cut(fullName, "/")
	if !found || name == "" {
		return fullName
	}
	return name
}

func init() {
	pairs := map[string][2]string{
		"approved.workspace":          {"工作空间", "Workspace"},
		"ui.github_account":           {"GitHub 账户", "GitHub account"},
		"ui.github_oauth":             {"GitHub OAuth", "GitHub OAuth"},
		"ui.local_workspace":          {"本地工作区", "Local workspace"},
		"ui.legacy_access":            {"本机管理模式", "Local management"},
		"ui.api_access":               {"Agent 访问", "Agent access"},
		"ui.unavailable":              {"未开放", "Unavailable"},
		"ui.api_unavailable_help":     {"AK/SK 与 JSON API 尚未接入真实后端。", "AK/SK and the JSON API do not have a production backend yet."},
		"ui.open_navigation":          {"展开导航", "Open navigation"},
		"ui.web_export_unavailable":   {"网页导出尚未开放；现有真实导出仍通过命令行完成。", "Web export is not available yet; production exports remain available from the CLI."},
		"ui.export":                   {"导出", "Export"},
		"approved.add_project":        {"添加项目", "Add project"},
		"ui.monitored_projects":       {"观察中的项目", "Monitored projects"},
		"ui.latest_observation":       {"最近观测", "Latest observation"},
		"ui.filters":                  {"筛选", "Filters"},
		"ui.view_mode":                {"阅读方式", "Reading mode"},
		"ui.reading_view":             {"阅读视图", "Reading view"},
		"ui.compact_view":             {"紧凑视图", "Compact view"},
		"ui.search_repository_or_tag": {"搜索仓库或精确标签", "Search repository or exact tag"},
		"ui.repository_name_az":       {"仓库名称 A–Z", "Repository name A–Z"},
		"ui.active_filters":           {"当前筛选", "Active filters"},
		"ui.actions":                  {"操作", "Actions"},
		"ui.watch":                    {"＋ 关注", "+ Watch"},
		"ui.watched":                  {"✓ 已关注", "✓ Watched"},
		"ui.browse_all_projects":      {"浏览全部项目", "Browse all projects"},
		"ui.projects_unit":            {"个项目", "projects"},
		"ui.per_page":                 {"每页", "Per page"},
		"ui.page_number":              {"第 %s 页", "Page %s"},
	}
	for key, pair := range pairs {
		messageCatalog[localeChinese][key] = pair[0]
		messageCatalog[localeEnglish][key] = pair[1]
	}
}
