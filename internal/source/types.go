package source

import "time"

// Repository is the source-layer representation of a GitHub repository. Stars
// is deliberately named AbsoluteStars so it cannot be confused with an OSS
// Insight rolling-window value.
type Repository struct {
	ID            int64
	NodeID        string
	FullName      string
	HTMLURL       string
	Description   string
	Language      string
	AbsoluteStars *int64
	Forks         *int64
	Fork          bool
	Archived      bool
	Private       bool
	GitHubStatus  string
	Topics        []string
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
	PushedAt      *time.Time
}

// Candidate is a repository reported by a discovery source. Source-specific
// evidence remains in the adapter result and can be mapped to domain records by
// the application layer.
type Candidate struct {
	Repository    Repository
	Source        string
	Profile       string
	DiscoveredAt  time.Time
	IsFocus       bool
	ManualNote    string
	MonitorStatus string
	PreviousNames []string
	Metadata      map[string]string
}

// Observation is one real, timestamped absolute-star observation imported from
// an existing system. Missing days are represented by missing rows, never by a
// zero value or an interpolated record.
type Observation struct {
	RepositoryID int64
	SnapshotDate time.Time
	ObservedAt   time.Time
	StarCount    int64
	Source       string
	FixedPanel   bool
	Metadata     map[string]string
}

// Warning records a rejected or lossy source row without aborting an otherwise
// useful import.
type Warning struct {
	Row     int
	Code    string
	Message string
}

// MergeCandidates deduplicates by the permanent GitHub repository ID. It keeps
// the first discovery provenance while filling missing metadata from later
// sources and recording every source in Metadata["discovery_sources"].
func MergeCandidates(groups ...[]Candidate) []Candidate {
	order := make([]int64, 0)
	merged := make(map[int64]Candidate)
	sources := make(map[int64]map[string]struct{})
	namePriorities := make(map[int64]int)

	for _, group := range groups {
		for _, candidate := range group {
			id := candidate.Repository.ID
			if id <= 0 {
				continue
			}
			current, ok := merged[id]
			if !ok {
				current = candidate
				current.Repository.Topics = append([]string(nil), candidate.Repository.Topics...)
				current.PreviousNames = append([]string(nil), candidate.PreviousNames...)
				current.Metadata = cloneMetadata(candidate.Metadata)
				merged[id] = current
				order = append(order, id)
				sources[id] = make(map[string]struct{})
				namePriorities[id] = sourcePriority(candidate.Source)
			}
			if candidate.Source != "" {
				sources[id][candidate.Source] = struct{}{}
			}
			if incomingName := candidate.Repository.FullName; incomingName != "" && incomingName != current.Repository.FullName {
				if sourcePriority(candidate.Source) >= namePriorities[id] {
					current.PreviousNames = appendUnique(current.PreviousNames, current.Repository.FullName)
					current.Repository.FullName = incomingName
					namePriorities[id] = sourcePriority(candidate.Source)
				} else {
					current.PreviousNames = appendUnique(current.PreviousNames, incomingName)
				}
			}
			candidate.Repository.FullName = current.Repository.FullName
			current = mergeCandidate(current, candidate)
			merged[id] = current
		}
	}

	result := make([]Candidate, 0, len(order))
	for _, id := range order {
		candidate := merged[id]
		candidate.Metadata = cloneMetadata(candidate.Metadata)
		candidate.Metadata["discovery_sources"] = joinSet(sources[id])
		result = append(result, candidate)
	}
	return result
}

func mergeCandidate(dst, src Candidate) Candidate {
	if dst.Repository.NodeID == "" {
		dst.Repository.NodeID = src.Repository.NodeID
	}
	if dst.Repository.HTMLURL == "" {
		dst.Repository.HTMLURL = src.Repository.HTMLURL
	}
	if dst.Repository.Description == "" {
		dst.Repository.Description = src.Repository.Description
	}
	if dst.Repository.Language == "" {
		dst.Repository.Language = src.Repository.Language
	}
	if src.Repository.AbsoluteStars != nil {
		dst.Repository.AbsoluteStars = src.Repository.AbsoluteStars
	}
	if src.Repository.Forks != nil {
		dst.Repository.Forks = src.Repository.Forks
	}
	dst.Repository.Archived = dst.Repository.Archived || src.Repository.Archived
	dst.Repository.Private = dst.Repository.Private || src.Repository.Private
	dst.Repository.Fork = dst.Repository.Fork || src.Repository.Fork
	if src.Repository.GitHubStatus != "" {
		dst.Repository.GitHubStatus = src.Repository.GitHubStatus
	}
	dst.Repository.Topics = appendUnique(dst.Repository.Topics, src.Repository.Topics...)
	if dst.Repository.CreatedAt == nil {
		dst.Repository.CreatedAt = src.Repository.CreatedAt
	}
	if src.Repository.UpdatedAt != nil {
		dst.Repository.UpdatedAt = src.Repository.UpdatedAt
	}
	if src.Repository.PushedAt != nil {
		dst.Repository.PushedAt = src.Repository.PushedAt
	}
	dst.IsFocus = dst.IsFocus || src.IsFocus
	if dst.ManualNote == "" {
		dst.ManualNote = src.ManualNote
	}
	if dst.MonitorStatus == "" {
		dst.MonitorStatus = src.MonitorStatus
	}
	dst.PreviousNames = appendUnique(dst.PreviousNames, src.PreviousNames...)
	if dst.Metadata == nil {
		dst.Metadata = make(map[string]string)
	}
	for key, value := range src.Metadata {
		if _, exists := dst.Metadata[key]; !exists {
			dst.Metadata[key] = value
		}
	}
	return dst
}

func sourcePriority(value string) int {
	switch value {
	case "github_search", "github-search", "github_trending", "manual":
		return 3
	case "ossinsight":
		return 2
	case "legacy":
		return 1
	default:
		return 0
	}
}

func cloneMetadata(input map[string]string) map[string]string {
	result := make(map[string]string, len(input)+1)
	for key, value := range input {
		result[key] = value
	}
	return result
}

func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	for _, value := range additions {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func joinSet(values map[string]struct{}) string {
	ordered := make([]string, 0, len(values))
	for value := range values {
		ordered = append(ordered, value)
	}
	// Source sets are tiny; insertion sort keeps this package dependency-free.
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j] < ordered[j-1]; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	result := ""
	for index, value := range ordered {
		if index > 0 {
			result += ","
		}
		result += value
	}
	return result
}
