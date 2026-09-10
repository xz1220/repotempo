package domain

import (
	"errors"
	"time"
)

var (
	ErrImportInvalid   = errors.New("import: invalid request")
	ErrImportBusy      = errors.New("import: queue full or conflicting pending request")
	ErrImportLeaseLost = errors.New("import: task lease no longer owned")
)

const (
	ImportJobType     = "repository_import"
	ImportQueueLimit  = 8
	ImportMaxAttempts = 3
)

type RepositoryImportRequest struct {
	OwnerUserID int64  `json:"owner_user_id,omitempty"`
	Repository  string `json:"repository"`
	Note        string `json:"note,omitempty"`
	TopicSlug   string `json:"topic_slug,omitempty"`
	Focus       bool   `json:"focus"`
}

// RepositoryReadme is an original-source extract, never a generated analysis.
type RepositoryReadme struct {
	RepositoryID int64     `json:"repository_id"`
	Intro        string    `json:"intro"`
	Headings     []string  `json:"headings"`
	HTMLURL      string    `json:"html_url"`
	SHA          string    `json:"sha"`
	Path         string    `json:"path"`
	FetchedAt    time.Time `json:"fetched_at"`
	Truncated    bool      `json:"truncated"`
	Text         string    `json:"text,omitempty"`
}

type RepositoryImportJob struct {
	ID            string                  `json:"id"`
	Request       RepositoryImportRequest `json:"request"`
	Stage         string                  `json:"stage"`
	RepositoryID  int64                   `json:"repository_id,omitempty"`
	FullName      string                  `json:"full_name,omitempty"`
	Created       bool                    `json:"created"`
	Attempts      int                     `json:"attempts"`
	QueuedAt      time.Time               `json:"queued_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
	NextAttemptAt *time.Time              `json:"next_attempt_at,omitempty"`
	FinishedAt    *time.Time              `json:"finished_at,omitempty"`
	ErrorCode     string                  `json:"error_code,omitempty"`
	Readme        *RepositoryReadme       `json:"readme,omitempty"`
}

func (job RepositoryImportJob) Terminal() bool {
	return job.Stage == "done" || job.Stage == "partial" || job.Stage == "failed"
}
