package domain

import "time"

// RepositoryActivity is a bounded cache of default-branch commit activity.
// It is independent of generated project readings and daily Star snapshots.
// A nil Repository.Activity means unknown, not an observed zero.
type RepositoryActivity struct {
	RepositoryID   int64         `json:"repository_id"`
	DefaultBranch  string        `json:"default_branch"`
	HeadSHA        string        `json:"head_sha"`
	IsFork         bool          `json:"is_fork"`
	WindowStart    time.Time     `json:"window_start"`
	WindowEnd      time.Time     `json:"window_end"`
	FetchedAt      time.Time     `json:"fetched_at"`
	LatestCommitAt *time.Time    `json:"latest_commit_at"`
	Commits        int           `json:"commits"`
	ActiveDays     int           `json:"active_days"`
	Complete       bool          `json:"complete"`
	Daily          []ActivityDay `json:"daily"`
}

// ActivityDay counts observed commits on one UTC calendar date.
type ActivityDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}
