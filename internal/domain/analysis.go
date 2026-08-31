package domain

import "time"

// RepositoryAnalysis is the latest durable interpretation of a repository.
// Revision and provenance identify the latest regenerated analysis, while the
// structured lists remain safe to render without parsing prose.
type RepositoryAnalysis struct {
	RepositoryID   int64     `json:"repository_id"`
	SummaryZH      string    `json:"summary_zh"`
	KeyPoints      []string  `json:"key_points"`
	UseCases       []string  `json:"use_cases"`
	TechnicalNotes string    `json:"technical_notes,omitempty"`
	Source         string    `json:"source"`
	Model          string    `json:"model,omitempty"`
	Revision       int       `json:"revision"`
	AnalyzedAt     time.Time `json:"analyzed_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
