package web

import (
	"strings"
	"time"
)

type repositoryAgeView struct {
	Label string
	Help  string
}

type activityPresentation struct {
	HasCache    bool
	Heading     string
	PeriodLabel string
	Summary     string
	LatestLabel string
	FetchLabel  string
	Branch      string
	CommitsURL  string
	Incomplete  bool
	IsFork      bool
	Bars        []activityBar
	ChartLabel  string
	StartLabel  string
	EndLabel    string
}

type activityBar struct {
	Date, Label   string
	X, Y, Height  int
	Unknown, Zero bool
}

func repositoryAge(created *time.Time, asOf time.Time, location *time.Location, l localizer) repositoryAgeView {
	view := repositoryAgeView{Label: l.Text("activity.age_unknown"), Help: l.Text("activity.age_help")}
	if created == nil || created.IsZero() || asOf.IsZero() {
		return view
	}
	if location == nil {
		location = time.UTC
	}
	// Compare calendar dates in the observation timezone, rather than treating
	// that date's midnight as proof that a repository created later is future.
	start, _ := time.Parse("2006-01-02", created.In(location).Format("2006-01-02"))
	end, _ := time.Parse("2006-01-02", asOf.In(location).Format("2006-01-02"))
	days := int(end.Sub(start).Hours() / 24)
	switch {
	case days < 0:
		view.Label = l.Text("activity.not_created")
	case days == 0:
		view.Label = l.Text("activity.created_today")
	case days == 1:
		view.Label = l.Text("activity.age_day")
	case days < 365:
		view.Label = l.Textf("activity.age_days", days)
	default:
		view.Label = l.Textf("activity.age_years", float64(days)/365.2425)
	}
	view.Help = l.Textf("activity.age_asof", end.Format("2006-01-02")) + " " + view.Help
	return view
}

// briefItems previews existing saved text without generating or changing it.
// Full arrays remain available on the repository detail page.
func briefItems(values []string) []string {
	result := make([]string, 0, 2)
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			continue
		}
		if len(result) == 2 {
			if !strings.HasSuffix(result[1], "…") {
				result[1] += "…"
			}
			break
		}
		runes := []rune(value)
		if len(runes) > 64 {
			value = string(runes[:64]) + "…"
		}
		result = append(result, value)
	}
	return result
}

func activityView(repository RepositoryMetric, asOf, now time.Time, l localizer) activityPresentation {
	view := activityPresentation{Heading: l.Text("activity.heading")}
	if root := projectGitHubURL(repository.FullName, ""); root != "" {
		view.CommitsURL = strings.TrimSuffix(root, "/") + "/commits"
	}
	value := repository.Activity
	if value == nil || value.RepositoryID != repository.ID || value.WindowStart.IsZero() || value.WindowEnd.IsZero() || value.FetchedAt.IsZero() || value.WindowEnd.Before(value.WindowStart) || value.Commits < 0 || value.ActiveDays < 0 {
		return view
	}
	view.HasCache = true
	view.Branch, view.Incomplete, view.IsFork = value.DefaultBranch, !value.Complete, value.IsFork
	cutoff := value.WindowEnd.UTC().Format("2006-01-02 15:04 UTC")
	view.Heading = l.Textf("activity.asof", cutoff)
	observationEnd := time.Date(asOf.Year(), asOf.Month(), asOf.Day()+1, 0, 0, 0, 0, asOf.Location())
	if !asOf.IsZero() && !value.FetchedAt.Before(observationEnd) && now.Sub(value.FetchedAt) <= 7*24*time.Hour {
		view.Heading = l.Textf("activity.latest_asof", cutoff)
	}
	view.PeriodLabel = l.Textf("activity.period", value.WindowStart.UTC().Format("2006-01-02"), value.WindowEnd.UTC().Format("2006-01-02"))
	view.Summary = l.Textf("activity.counts", value.Commits, value.ActiveDays)
	if !value.Complete {
		view.Summary = l.Textf("activity.minimum_counts", value.Commits, value.ActiveDays)
	}
	view.FetchLabel = l.Textf("activity.fetched", value.FetchedAt.UTC().Format("2006-01-02 15:04 UTC"))
	view.LatestLabel = l.Text("activity.latest_unknown")
	if value.LatestCommitAt != nil && !value.LatestCommitAt.IsZero() && !value.LatestCommitAt.After(value.WindowEnd) {
		view.LatestLabel = l.Textf("activity.latest_commit", value.LatestCommitAt.UTC().Format("2006-01-02"))
	}
	view.StartLabel, view.EndLabel = value.WindowStart.UTC().Format("01-02"), value.WindowEnd.UTC().Format("01-02")
	view.ChartLabel = view.PeriodLabel + ". " + view.Summary
	if !value.Complete {
		view.ChartLabel += ". " + l.Text("activity.incomplete")
	}
	view.Bars = activityBars(value, l)
	return view
}

func activityBars(value *RepositoryActivity, l localizer) []activityBar {
	// Only explicit rows count as observations. Missing days are never invented
	// as zero, including if a malformed or older cache claims completeness.
	counts := make(map[string]int, len(value.Daily))
	maximum := 1
	for _, day := range value.Daily {
		if day.Count >= 0 {
			counts[day.Date] = day.Count
			maximum = max(maximum, day.Count)
		}
	}
	start := time.Date(value.WindowStart.UTC().Year(), value.WindowStart.UTC().Month(), value.WindowStart.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := value.WindowEnd.UTC()
	result := make([]activityBar, 0, 30)
	for day := start; !day.After(end) && len(result) < 30; day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		count, exists := counts[date]
		bar := activityBar{Date: date, X: len(result)*10 + 1, Height: 2, Y: 46, Unknown: !exists || (!value.Complete && count == 0), Zero: exists && value.Complete && count == 0}
		switch {
		case bar.Unknown:
			bar.Label = l.Text("activity.day_unknown")
		case !value.Complete:
			bar.Label = l.Textf("activity.day_minimum", count)
		default:
			bar.Label = l.Textf("activity.day_count", count)
		}
		if count > 0 {
			bar.Height = max(2, int(float64(count)/float64(maximum)*44))
			bar.Y = 48 - bar.Height
		}
		result = append(result, bar)
	}
	return result
}

func init() {
	pairs := map[string][2]string{
		"activity.purpose":        {"项目用途", "Purpose"},
		"activity.capabilities":   {"核心能力", "Capabilities"},
		"activity.use_cases":      {"适用场景", "Use cases"},
		"activity.age":            {"仓库年龄", "Repository age"},
		"activity.age_unknown":    {"创建时间未知", "Creation date unknown"},
		"activity.age_help":       {"仓库年龄仅表示距 GitHub 创建日期的时间，不代表持续开发时长。", "Repository age measures time since GitHub creation, not continuous development."},
		"activity.age_asof":       {"截至观测日期 %s。", "As of observation date %s."},
		"activity.age_days":       {"%d 天", "%d days"},
		"activity.age_day":        {"1 天", "1 day"},
		"activity.age_years":      {"约 %.1f 年", "About %.1f years"},
		"activity.not_created":    {"观测日期时尚未创建", "Not yet created on the observation date"},
		"activity.created_today":  {"观测当天创建", "Created on the observation date"},
		"activity.heading":        {"提交活动", "Commit activity"},
		"activity.asof":           {"提交活动 · 截至 %s", "Commit activity · as of %s"},
		"activity.latest_asof":    {"最新开发活动 · 截至 %s", "Latest development activity · as of %s"},
		"activity.period":         {"%s 至 %s · 30 个 UTC 日期，截止日仅统计到上述时间", "%s to %s · 30 UTC dates; final day is partial"},
		"activity.card_window":    {"近 30 个 UTC 日期", "30 UTC dates"},
		"activity.counts":         {"%d 次提交 · %d 个活跃日", "%d commits · %d active days"},
		"activity.minimum_counts": {"至少 %d 次提交 · 至少 %d 个活跃日", "At least %d commits · at least %d active days"},
		"activity.fetched":        {"采集于 %s", "Fetched %s"},
		"activity.latest_commit":  {"最近提交 %s（UTC）", "Latest commit %s (UTC)"},
		"activity.latest_unknown": {"最近提交时间未知", "Latest commit time unknown"},
		"activity.source":         {"GitHub API · 默认分支", "GitHub API · default branch"},
		"activity.view_commits":   {"查看 GitHub 提交记录", "View commits on GitHub"},
		"activity.incomplete":     {"仅采到部分提交（最多 500 条），提交数与活跃天数都是下限；空白日期不代表没有提交。", "Partial sample (up to 500 commits). Commit and active-day totals are lower bounds; blank dates do not mean no commits."},
		"activity.partial":        {"部分采样", "Partial sample"},
		"activity.fork":           {"Fork 仓库，提交记录可能包含上游历史。", "Fork repository; commit history may include upstream commits."},
		"activity.day_unknown":    {"未完整覆盖", "Not fully covered"},
		"activity.day_minimum":    {"至少 %d 次提交", "At least %d commits"},
		"activity.day_count":      {"%d 次提交", "%d commits"},
		"activity.daily_details":  {"查看逐日提交数", "View daily commit counts"},
	}
	for key, pair := range pairs {
		messageCatalog[localeChinese][key] = pair[0]
		messageCatalog[localeEnglish][key] = pair[1]
	}
}
