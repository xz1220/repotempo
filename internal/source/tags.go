package source

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CloneTags retains the distinction between absent topics and a known empty list.
func CloneTags(tags []string) []string {
	if tags == nil {
		return nil
	}
	return append([]string{}, tags...)
}

func ParseTagsJSON(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	if !strings.HasPrefix(raw, "[") {
		return nil, fmt.Errorf("tags must be a JSON string array")
	}
	var values []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(values))
	for _, value := range values {
		if string(value) == "null" {
			return nil, fmt.Errorf("tag must be a string")
		}
		var tag string
		if err := json.Unmarshal(value, &tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}
