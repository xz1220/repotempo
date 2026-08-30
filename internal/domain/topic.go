package domain

import "time"

type TopicStatus string

const (
	TopicActive   TopicStatus = "active"
	TopicArchived TopicStatus = "archived"
)

func (status TopicStatus) Valid() bool {
	return status == TopicActive || status == TopicArchived
}

type Topic struct {
	ID          int64       `json:"id"`
	Slug        string      `json:"slug"`
	Name        string      `json:"name"`
	ParentID    *int64      `json:"parent_id,omitempty"`
	Description string      `json:"description,omitempty"`
	Status      TopicStatus `json:"status"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type TopicSource string

const (
	TopicSourceManual   TopicSource = "manual"
	TopicSourceGitHub   TopicSource = "github"
	TopicSourceImported TopicSource = "imported"
	TopicSourceAuto     TopicSource = "auto"
)

func (source TopicSource) Valid() bool {
	switch source {
	case TopicSourceManual, TopicSourceGitHub, TopicSourceImported, TopicSourceAuto:
		return true
	default:
		return false
	}
}

type RepositoryTopic struct {
	RepositoryID int64       `json:"repository_id"`
	TopicID      int64       `json:"topic_id"`
	Source       TopicSource `json:"source"`
	Confirmed    bool        `json:"confirmed"`
	Confidence   *float64    `json:"confidence,omitempty"`
	AssignedAt   time.Time   `json:"assigned_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

type TopicAssignmentResult struct {
	Assignment RepositoryTopic `json:"assignment"`
	Changed    bool            `json:"changed"`
	Protected  bool            `json:"protected"`
}
