package web

func init() {
	for key, pair := range map[string][2]string{
		"daily.today":       {"当天新入库", "Today's additions"},
		"daily.on_date":     {"%s 新入库", "New on %s"},
		"daily.views":       {"项目库视图", "Project library views"},
		"daily.empty_title": {"%s 没有符合条件的新入库项目", "No matching additions on %s"},
		"daily.empty_help":  {"这一天尚未发现新项目，或当前筛选没有匹配项。你可以查看全部项目，也可以切换日期或调整筛选。", "There are no additions on this date, or none match these filters. Browse all projects, choose another date, or adjust the filters."},
		"daily.browse_all":  {"查看全部项目", "Browse all projects"},
	} {
		messageCatalog[localeChinese][key], messageCatalog[localeEnglish][key] = pair[0], pair[1]
	}
}
