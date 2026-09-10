package web

import (
	"net/url"
	"sort"
	"strings"
)

type cardTagLinks struct {
	Visible []viewOption
	More    []viewOption
}

// Keep tags navigable without loading every chip into the first reading view.
// The complete set remains in the markup and all filtering happens in SQLite.
func makeCardTagLinks(tags []string, topics []TopicRef, values url.Values, l localizer) cardTagLinks {
	items := []viewOption{}
	seen := map[string]int{}
	add := func(value, label string, aliases ...string) {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			return
		}
		selected := strings.TrimSpace(values.Get("tag"))
		active := strings.EqualFold(selected, key)
		for _, alias := range aliases {
			active = active || strings.EqualFold(selected, strings.TrimSpace(alias))
		}
		if index, exists := seen[key]; exists {
			items[index].Active = items[index].Active || active
			return
		}
		seen[key] = len(items)
		query := cloneValues(values)
		query.Del("cursor")
		query.Del("page")
		query.Set("tag", key)
		items = append(items, viewOption{Label: label, URL: queryPath("/repositories", query), Active: active})
	}
	for _, tag := range tags {
		add(tag, tag)
	}
	for _, topic := range topics {
		add(topic.Slug, l.TopicName(topic.Slug, topic.Name), topic.Name)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Active && !items[j].Active })
	if len(items) > 4 {
		return cardTagLinks{Visible: items[:4], More: items[4:]}
	}
	return cardTagLinks{Visible: items}
}

func tagOptionLabel(tag string, locale string) string {
	if locale == localeChinese {
		switch tag {
		case "investment":
			return "investment · 投资"
		case "skills":
			return "skills · 技能"
		case "saas":
			return "saas · 软件服务"
		}
	}
	return tag
}

func suggestedTagLinks(tags []TagRef, values url.Values, l localizer) []viewOption {
	available := map[string]bool{}
	for _, tag := range tags {
		available[tag.Name] = true
	}
	links := []viewOption{}
	for _, name := range []string{"investment", "agent", "skills", "saas"} {
		if !available[name] {
			continue
		}
		query := cloneValues(values)
		query.Del("cursor")
		query.Del("page")
		query.Set("tag", name)
		links = append(links, viewOption{Label: l.Text("tags.quick." + name), URL: queryPath("/repositories", query), Active: strings.EqualFold(strings.TrimSpace(values.Get("tag")), name)})
	}
	return links
}

func init() {
	for key, pair := range map[string][2]string{
		"tags.quick":               {"常用标签", "Common tags"},
		"tags.quick.investment":    {"投资 · investment", "investment"},
		"tags.quick.agent":         {"Agent", "Agent"},
		"tags.quick.skills":        {"Skills", "Skills"},
		"tags.quick.saas":          {"SaaS", "SaaS"},
		"tags.filter":              {"标签", "Tag"},
		"tags.placeholder":         {"输入或选择标签", "Type or select a tag"},
		"tags.help":                {"支持项目原始标签、旧日报标签和已有分类；点击卡片标签也可筛选。", "Use repository tags, archived tags, or existing categories. Card tags are clickable filters."},
		"tags.period":              {"增长对比", "Compare growth"},
		"tags.1d":                  {"与 1 天前比", "1 day earlier"},
		"tags.7d":                  {"与 7 天前比", "7 days earlier"},
		"tags.30d":                 {"与 30 天前比", "30 days earlier"},
		"tags.period_help":         {"以观测日期为终点，比较 Star 数的变化。", "Compare Star counts ending on the observation date."},
		"tags.method_note":         {"标签反映当前保存的项目属性；观测日期用于入库范围和 Star 历史，不回溯标签变更。", "Tags reflect the currently saved project metadata. The observation date scopes repository entry and Star history, not historical tag versions."},
		"tags.comparison":          {"增长对比：%s 至 %s", "Growth comparison: %s to %s"},
		"tags.none":                {"暂无标签", "No tags yet"},
		"tags.more":                {"展开另外 %d 个标签", "Show %d more tags"},
		"tags.clear":               {"清除筛选", "Clear filter"},
		"tags.browse_all_matching": {"查看全库匹配项目", "Find matching projects in the full library"},
		"tags.active":              {"标签：%s", "Tag: %s"},
		"tags.legacy_topic":        {"分类：%s", "Category: %s"},
		"tags.legacy_source":       {"旧链接来源筛选：%s", "Source filter from this link: %s"},
		"tags.invalid":             {"标签格式不正确", "Invalid tag"},
		"tags.invalid_help":        {"标签不能超过 80 个字符，也不能包含控制字符。", "Use a tag of up to 80 characters without control characters."},
	} {
		messageCatalog[localeChinese][key], messageCatalog[localeEnglish][key] = pair[0], pair[1]
	}
}
