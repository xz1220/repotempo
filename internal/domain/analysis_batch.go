package domain

import "time"

const MaxAnalysisBatchProjects = 5000

// AnalysisImportProject identifies an existing registry entry by full name.
// RepositoryID, when supplied, is an additional identity check, not a fallback.
type AnalysisImportProject struct {
	FullName       string
	RepositoryID   *int64
	SummaryZH      string
	KeyPoints      []string
	UseCases       []string
	TechnicalNotes string
	Source         string
	Model          string
	AnalyzedAt     time.Time
}

type AnalysisBatchResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}
