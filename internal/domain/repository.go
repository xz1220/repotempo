package domain

import "time"

type DiscoverySource string

const (
	DiscoverySourceOSSInsight     DiscoverySource = "ossinsight"
	DiscoverySourceGitHubSearch   DiscoverySource = "github_search"
	DiscoverySourceGitHubTrending DiscoverySource = "github_trending"
	DiscoverySourceLegacy         DiscoverySource = "legacy"
	DiscoverySourceManual         DiscoverySource = "manual"
)

func (source DiscoverySource) Valid() bool {
	switch source {
	case DiscoverySourceOSSInsight, DiscoverySourceGitHubSearch, DiscoverySourceGitHubTrending, DiscoverySourceLegacy, DiscoverySourceManual:
		return true
	default:
		return false
	}
}

type MonitoringStatus string

const (
	MonitoringActive  MonitoringStatus = "active"
	MonitoringPaused  MonitoringStatus = "paused"
	MonitoringStopped MonitoringStatus = "stopped"
)

func (status MonitoringStatus) Valid() bool {
	switch status {
	case MonitoringActive, MonitoringPaused, MonitoringStopped:
		return true
	default:
		return false
	}
}

type GitHubStatus string

const (
	GitHubActive      GitHubStatus = "active"
	GitHubArchived    GitHubStatus = "archived"
	GitHubDeleted     GitHubStatus = "deleted"
	GitHubPrivate     GitHubStatus = "private"
	GitHubUnreachable GitHubStatus = "unreachable"
)

func (status GitHubStatus) Valid() bool {
	switch status {
	case GitHubActive, GitHubArchived, GitHubDeleted, GitHubPrivate, GitHubUnreachable:
		return true
	default:
		return false
	}
}

// Repository is the durable registry entry keyed by GitHub's immutable
// repository ID. Current stars intentionally do not belong to this model.
type Repository struct {
	GitHubRepoID     int64             `json:"github_repo_id"`
	GitHubNodeID     string            `json:"github_node_id,omitempty"`
	FullName         string            `json:"full_name"`
	HTMLURL          string            `json:"html_url,omitempty"`
	Description      string            `json:"description,omitempty"`
	PrimaryLanguage  string            `json:"primary_language,omitempty"`
	GitHubTopics     []string          `json:"github_topics"`
	ResearchTags     []string          `json:"research_tags"`
	GitHubCreatedAt  *time.Time        `json:"github_created_at,omitempty"`
	FirstSeenAt      time.Time         `json:"first_seen_at"`
	FirstSeenSource  DiscoverySource   `json:"first_seen_source"`
	FirstSeenProfile string            `json:"first_seen_profile,omitempty"`
	DiscoverySources []DiscoverySource `json:"discovery_sources"`
	LastDiscoveredAt time.Time         `json:"last_discovered_at"`
	MonitoringStatus MonitoringStatus  `json:"monitoring_status"`
	GitHubStatus     GitHubStatus      `json:"github_status"`
	IsFocus          bool              `json:"is_focus"`
	ManualNote       string            `json:"manual_note,omitempty"`
	GitHubETag       string            `json:"github_etag,omitempty"`
	LastCheckedAt    *time.Time        `json:"last_checked_at,omitempty"`
	PreviousNames    []string          `json:"previous_names"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// RepositoryObservation is an observation from one discovery source. Pointer
// fields distinguish "not supplied" from a deliberate empty value.
type RepositoryObservation struct {
	GitHubRepoID     int64
	GitHubNodeID     string
	FullName         string
	HTMLURL          string
	Description      *string
	PrimaryLanguage  *string
	GitHubTopics     *[]string
	ResearchTags     *[]string
	GitHubCreatedAt  *time.Time
	Source           DiscoverySource
	Profile          string
	DiscoveredAt     time.Time
	MonitoringStatus MonitoringStatus
	GitHubStatus     GitHubStatus
	IsFocus          *bool
	ManualNote       *string
	GitHubETag       *string
	LastCheckedAt    *time.Time
}

type RepositoryFilter struct {
	MonitoringStatus MonitoringStatus
	GitHubStatus     GitHubStatus
	DiscoverySource  DiscoverySource
	FocusOnly        bool
	Limit            int
	Offset           int
}
