package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

var topicSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Topics struct {
	Version int               `yaml:"version"`
	Topics  []TopicDefinition `yaml:"topics"`
}

type TopicDefinition struct {
	Slug        string            `yaml:"slug"`
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Status      string            `yaml:"status"`
	Children    []TopicDefinition `yaml:"children"`
}

// FlatTopic is an import-ready topic definition. ParentSlug is empty for a
// root topic and set for a second-level topic.
type FlatTopic struct {
	Slug        string
	Name        string
	Description string
	Status      string
	ParentSlug  string
}

func LoadTopics(path string) (Topics, error) {
	file, err := os.Open(path)
	if err != nil {
		return Topics{}, fmt.Errorf("open topics config: %w", err)
	}
	defer file.Close()
	topics, err := DecodeTopics(file)
	if err != nil {
		return Topics{}, fmt.Errorf("decode topics config %q: %w", path, err)
	}
	return topics, nil
}

func DecodeTopics(reader io.Reader) (Topics, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var topics Topics
	if err := decoder.Decode(&topics); err != nil {
		return Topics{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Topics{}, errors.New("topics configuration must contain one YAML document")
		}
		return Topics{}, err
	}
	if err := topics.Validate(); err != nil {
		return Topics{}, err
	}
	return topics, nil
}

func (topics Topics) Validate() error {
	if topics.Version != 1 {
		return fmt.Errorf("unsupported topics config version %d", topics.Version)
	}
	if len(topics.Topics) == 0 {
		return errors.New("topics must not be empty")
	}
	seen := make(map[string]string)
	for index, topic := range topics.Topics {
		path := fmt.Sprintf("topics[%d]", index)
		if err := validateTopic(topic, path, 1, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateTopic(topic TopicDefinition, path string, depth int, seen map[string]string) error {
	if !topicSlugPattern.MatchString(topic.Slug) {
		return fmt.Errorf("%s.slug %q must contain lowercase letters, digits, and single hyphens", path, topic.Slug)
	}
	if previous, duplicate := seen[topic.Slug]; duplicate {
		return fmt.Errorf("duplicate topic slug %q at %s (already used at %s)", topic.Slug, path, previous)
	}
	seen[topic.Slug] = path
	if topic.Name == "" {
		return fmt.Errorf("%s.name is required", path)
	}
	switch topic.Status {
	case "active", "archived":
	default:
		return fmt.Errorf("%s.status must be active or archived", path)
	}
	if depth >= 2 && len(topic.Children) > 0 {
		return fmt.Errorf("%s.children exceeds the two-level topic limit", path)
	}
	for index, child := range topic.Children {
		childPath := fmt.Sprintf("%s.children[%d]", path, index)
		if err := validateTopic(child, childPath, depth+1, seen); err != nil {
			return err
		}
	}
	return nil
}

func (topics Topics) Flatten() []FlatTopic {
	result := make([]FlatTopic, 0)
	for _, topic := range topics.Topics {
		result = append(result, flatTopic(topic, ""))
		for _, child := range topic.Children {
			result = append(result, flatTopic(child, topic.Slug))
		}
	}
	return result
}

func flatTopic(topic TopicDefinition, parent string) FlatTopic {
	return FlatTopic{
		Slug:        topic.Slug,
		Name:        topic.Name,
		Description: topic.Description,
		Status:      topic.Status,
		ParentSlug:  parent,
	}
}
