package web

func init() {
	for key, values := range map[string][2]string{
		"dashboard.top_growth":      {"涨势最快 Top 10", "Fastest growth · Top 10"},
		"dashboard.top_growth_help": {"按本周期新增 Star 排序，只展示正增长项目。", "The largest positive Star gains in this period."},
		"dashboard.slowdown_help":   {"本周期新增 Star 比上一周期减少的项目。", "Projects gaining fewer Stars than in the preceding period."},
		"dashboard.scope":           {"观测范围 %s 个项目 · %s 个可对比", "%s tracked projects · %s comparable"},
		"dashboard.new_projects":    {"当天新入库 %s 个", "%s new projects on this date"},
		"dashboard.view_board":      {"查看全部：%s", "View all: %s"},
		"dashboard.no_growth":       {"这个周期还没有可展示的正增长项目", "No comparable positive growth in this period"},
		"dashboard.no_slowdown":     {"这个周期还没有可展示的势头回落项目", "No comparable slowdown in this period"},
		"dashboard.empty_help":      {"可以换一个日期、周期或主题继续查看。", "Try another date, period, or category."},
		"dashboard.methodology":     {"增长比较所选周期起止两天的 Star 数，缺失观测不按零处理。势头回落比较前后两个等长周期，需要三个日期都有观测；它不等于 Star 总数下降。", "Growth compares Stars at both endpoints; missing observations are not zero. A slowdown compares two equal periods with three observed dates. It does not necessarily mean the total Star count fell."},
	} {
		messageCatalog[localeChinese][key] = values[0]
		messageCatalog[localeEnglish][key] = values[1]
	}
}
