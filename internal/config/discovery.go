package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a YAML duration such as "250ms", "2s", or "1m".
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", raw, err)
	}
	if parsed < 0 {
		return fmt.Errorf("duration must not be negative: %q", raw)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Value() time.Duration { return time.Duration(d) }

type Discovery struct {
	Version    int             `yaml:"version"`
	GitHub     GitHub          `yaml:"github"`
	OSSInsight OSSInsight      `yaml:"ossinsight"`
	Profiles   []SearchProfile `yaml:"profiles"`
	Manual     Manual          `yaml:"manual"`
	Legacy     Legacy          `yaml:"legacy"`
}

type GitHub struct {
	APIBaseURL       string   `yaml:"api_base_url"`
	APIVersion       string   `yaml:"api_version"`
	UserAgent        string   `yaml:"user_agent"`
	Timeout          Duration `yaml:"timeout"`
	CoreInterval     Duration `yaml:"core_interval"`
	SearchInterval   Duration `yaml:"search_interval"`
	SecondaryBackoff Duration `yaml:"secondary_backoff"`
	ServerBackoff    Duration `yaml:"server_backoff"`
	MaxRetries       int      `yaml:"max_retries"`
}

type OSSInsight struct {
	Enabled  *bool       `yaml:"enabled"`
	BaseURL  string      `yaml:"base_url"`
	Language string      `yaml:"language"`
	Timeout  Duration    `yaml:"timeout"`
	Windows  []OSSWindow `yaml:"windows"`
}

type OSSWindow struct {
	Name   string `yaml:"name"`
	Period string `yaml:"period"`
}

type SearchProfile struct {
	Name      string        `yaml:"name"`
	Enabled   *bool         `yaml:"enabled"`
	Schedule  string        `yaml:"schedule"`
	Sort      string        `yaml:"sort"`
	Order     string        `yaml:"order"`
	Queries   []SearchQuery `yaml:"queries"`
	Partition Partition     `yaml:"partition"`
}

func (p SearchProfile) IsEnabled() bool { return p.Enabled == nil || *p.Enabled }

type SearchQuery struct {
	Text     string      `yaml:"text"`
	Stars    NumberRange `yaml:"stars"`
	Topic    string      `yaml:"topic"`
	Created  DateRange   `yaml:"created"`
	Pushed   DateRange   `yaml:"pushed"`
	Language string      `yaml:"language"`
	Fork     *bool       `yaml:"fork"`
	Archived *bool       `yaml:"archived"`
}

type NumberRange struct {
	Min *int64 `yaml:"min"`
	Max *int64 `yaml:"max"`
}

type DateRange struct {
	From      string `yaml:"from"`
	To        string `yaml:"to"`
	SinceDays int    `yaml:"since_days"`
}

// Resolve returns an inclusive date range. A relative lower bound is resolved
// using the supplied clock, which keeps date partitioning deterministic in
// tests and avoids baking moving dates into configuration files.
func (r DateRange) Resolve(now time.Time) (*time.Time, *time.Time, error) {
	if r.SinceDays < 0 {
		return nil, nil, errors.New("since_days must not be negative")
	}
	if r.SinceDays > 0 && r.From != "" {
		return nil, nil, errors.New("from and since_days are mutually exclusive")
	}
	parse := func(value string) (*time.Time, error) {
		if value == "" {
			return nil, nil
		}
		parsed, err := time.ParseInLocation("2006-01-02", value, now.Location())
		if err != nil {
			return nil, fmt.Errorf("date %q must use YYYY-MM-DD: %w", value, err)
		}
		return &parsed, nil
	}
	from, err := parse(r.From)
	if err != nil {
		return nil, nil, err
	}
	to, err := parse(r.To)
	if err != nil {
		return nil, nil, err
	}
	if r.SinceDays > 0 {
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -r.SinceDays)
		from = &start
	}
	if from != nil && to != nil && from.After(*to) {
		return nil, nil, errors.New("date range from must not be after to")
	}
	return from, to, nil
}

type Partition struct {
	By       string `yaml:"by"`
	MaxDepth int    `yaml:"max_depth"`
}

type Manual struct {
	Files []string `yaml:"files"`
}

type Legacy struct {
	DatabasePath string `yaml:"database_path"`
}

func LoadDiscovery(path string) (Discovery, error) {
	file, err := os.Open(path)
	if err != nil {
		return Discovery{}, fmt.Errorf("open discovery config: %w", err)
	}
	defer file.Close()
	config, err := DecodeDiscovery(file)
	if err != nil {
		return Discovery{}, fmt.Errorf("decode discovery config %q: %w", path, err)
	}
	return config, nil
}

func DecodeDiscovery(reader io.Reader) (Discovery, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var config Discovery
	if err := decoder.Decode(&config); err != nil {
		return Discovery{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Discovery{}, errors.New("configuration must contain one YAML document")
		}
		return Discovery{}, err
	}
	if err := config.Validate(); err != nil {
		return Discovery{}, err
	}
	return config, nil
}

func (c Discovery) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported discovery config version %d", c.Version)
	}
	if err := validateURL("github.api_base_url", c.GitHub.APIBaseURL); err != nil {
		return err
	}
	if c.GitHub.UserAgent == "" {
		return errors.New("github.user_agent is required")
	}
	if c.GitHub.Timeout.Value() <= 0 {
		return errors.New("github.timeout must be positive")
	}
	if c.GitHub.SearchInterval.Value() <= 0 {
		return errors.New("github.search_interval must be positive")
	}
	if c.GitHub.SecondaryBackoff.Value() < time.Minute {
		return errors.New("github.secondary_backoff must be at least 1m")
	}
	if c.GitHub.ServerBackoff.Value() <= 0 {
		return errors.New("github.server_backoff must be positive")
	}
	if c.GitHub.MaxRetries < 0 {
		return errors.New("github.max_retries must not be negative")
	}
	if c.GitHub.MaxRetries > 5 {
		return errors.New("github.max_retries must not exceed 5")
	}
	if c.OSSInsight.Enabled == nil || *c.OSSInsight.Enabled {
		if err := validateURL("ossinsight.base_url", c.OSSInsight.BaseURL); err != nil {
			return err
		}
		if c.OSSInsight.Timeout.Value() <= 0 {
			return errors.New("ossinsight.timeout must be positive")
		}
		if len(c.OSSInsight.Windows) == 0 {
			return errors.New("ossinsight.windows must not be empty when enabled")
		}
	}
	periods := map[string]bool{
		"past_24_hours": true,
		"past_week":     true,
		"past_month":    true,
		"past_3_months": true,
	}
	windowNames := make(map[string]bool)
	for index, window := range c.OSSInsight.Windows {
		if window.Name == "" {
			return fmt.Errorf("ossinsight.windows[%d].name is required", index)
		}
		if windowNames[window.Name] {
			return fmt.Errorf("duplicate OSS Insight window name %q", window.Name)
		}
		windowNames[window.Name] = true
		if !periods[window.Period] {
			return fmt.Errorf("ossinsight.windows[%d].period %q is unsupported", index, window.Period)
		}
	}

	profileNames := make(map[string]bool)
	for index, profile := range c.Profiles {
		if err := profile.validate(index); err != nil {
			return err
		}
		if profileNames[profile.Name] {
			return fmt.Errorf("duplicate search profile name %q", profile.Name)
		}
		profileNames[profile.Name] = true
	}
	return nil
}

func (p SearchProfile) validate(index int) error {
	prefix := fmt.Sprintf("profiles[%d]", index)
	if p.Name == "" {
		return fmt.Errorf("%s.name is required", prefix)
	}
	switch p.Schedule {
	case "daily", "weekly", "monthly", "always", "manual":
	default:
		return fmt.Errorf("%s.schedule %q is unsupported", prefix, p.Schedule)
	}
	switch p.Sort {
	case "stars", "updated":
	default:
		return fmt.Errorf("%s.sort must be stars or updated", prefix)
	}
	switch p.Order {
	case "asc", "desc":
	default:
		return fmt.Errorf("%s.order must be asc or desc", prefix)
	}
	if len(p.Queries) == 0 {
		return fmt.Errorf("%s.queries must not be empty", prefix)
	}
	switch p.Partition.By {
	case "none", "stars", "created", "pushed":
	default:
		return fmt.Errorf("%s.partition.by %q is unsupported", prefix, p.Partition.By)
	}
	if p.Partition.By != "none" && p.Partition.MaxDepth <= 0 {
		return fmt.Errorf("%s.partition.max_depth must be positive", prefix)
	}
	for queryIndex, query := range p.Queries {
		if err := query.validate(); err != nil {
			return fmt.Errorf("%s.queries[%d]: %w", prefix, queryIndex, err)
		}
		switch p.Partition.By {
		case "stars":
			if query.Stars.Min == nil && query.Stars.Max == nil {
				return fmt.Errorf("%s.queries[%d] needs a stars range for star partitioning", prefix, queryIndex)
			}
		case "created":
			if query.Created.From == "" && query.Created.SinceDays == 0 {
				return fmt.Errorf("%s.queries[%d] needs a created lower bound", prefix, queryIndex)
			}
		case "pushed":
			if query.Pushed.From == "" && query.Pushed.SinceDays == 0 {
				return fmt.Errorf("%s.queries[%d] needs a pushed lower bound", prefix, queryIndex)
			}
		}
	}
	return nil
}

func (q SearchQuery) validate() error {
	if q.Stars.Min != nil && *q.Stars.Min < 0 || q.Stars.Max != nil && *q.Stars.Max < 0 {
		return errors.New("stars bounds must not be negative")
	}
	if q.Stars.Min != nil && q.Stars.Max != nil && *q.Stars.Min > *q.Stars.Max {
		return errors.New("stars.min must not exceed stars.max")
	}
	now := time.Date(2000, 1, 31, 0, 0, 0, 0, time.UTC)
	if _, _, err := q.Created.Resolve(now); err != nil {
		return fmt.Errorf("created: %w", err)
	}
	if _, _, err := q.Pushed.Resolve(now); err != nil {
		return fmt.Errorf("pushed: %w", err)
	}
	if strings.TrimSpace(q.Text) == "" && q.Stars.Min == nil && q.Stars.Max == nil && q.Topic == "" && q.Created.From == "" && q.Created.To == "" && q.Created.SinceDays == 0 && q.Pushed.From == "" && q.Pushed.To == "" && q.Pushed.SinceDays == 0 && q.Language == "" {
		return errors.New("query must contain at least one search term or qualifier")
	}
	return nil
}

func validateURL(field, value string) error {
	if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL", field)
	}
	return nil
}
