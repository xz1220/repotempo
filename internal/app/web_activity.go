package app

import (
	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/web"
)

func mapRepositoryActivity(value *domain.RepositoryActivity) *web.RepositoryActivity {
	if value == nil {
		return nil
	}
	result := &web.RepositoryActivity{
		RepositoryID: value.RepositoryID, DefaultBranch: value.DefaultBranch,
		HeadSHA: value.HeadSHA, IsFork: value.IsFork,
		WindowStart: value.WindowStart, WindowEnd: value.WindowEnd, FetchedAt: value.FetchedAt,
		Commits: value.Commits, ActiveDays: value.ActiveDays, Complete: value.Complete,
		Daily: make([]web.ActivityDay, len(value.Daily)),
	}
	if value.LatestCommitAt != nil {
		latest := *value.LatestCommitAt
		result.LatestCommitAt = &latest
	}
	for index, day := range value.Daily {
		result.Daily[index] = web.ActivityDay{Date: day.Date, Count: day.Count}
	}
	return result
}
