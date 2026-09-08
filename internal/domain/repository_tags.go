package domain

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxRepositoryTags = 64
const MaxRepositoryTagRunes = 80

// NormalizeRepositoryTags keeps labels rather than inventing taxonomy slugs.
// Nil remains unknown; a supplied empty array remains a known empty array.
func NormalizeRepositoryTags(tags []string) ([]string, error) {
	if tags == nil {
		return nil, nil
	}
	if len(tags) > 256 {
		return nil, fmt.Errorf("tag input exceeds 256 entries")
	}
	result := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		if !utf8.ValidString(tag) {
			return nil, fmt.Errorf("tag must be valid UTF-8")
		}
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		for _, char := range tag {
			if unicode.IsControl(char) {
				return nil, fmt.Errorf("tag must not contain control characters")
			}
		}
		if !utf8.ValidString(tag) || utf8.RuneCountInString(tag) > MaxRepositoryTagRunes {
			return nil, fmt.Errorf("tag must be valid UTF-8 with at most %d characters", MaxRepositoryTagRunes)
		}
		if !seen[tag] {
			result = append(result, tag)
			seen[tag] = true
		}
	}
	if len(result) > MaxRepositoryTags {
		return nil, fmt.Errorf("repository exceeds %d unique tags", MaxRepositoryTags)
	}
	return result, nil
}

type RepositoryTagsImport struct {
	RepositoryID int64
	FullName     string
	GitHubTopics *[]string
	ResearchTags *[]string
}

type TagsBatchResult struct {
	Updated            int `json:"updated"`
	Skipped            int `json:"skipped"`
	GitHubTopicsFilled int `json:"github_topics_filled"`
	ResearchTagsAdded  int `json:"research_tags_added"`
}
