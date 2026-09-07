// Package classification assigns explainable product categories from repository
// metadata. It does not treat popularity, a search hit, or the word "skills" as
// evidence that a repository is an AI agent.
package classification

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	corestore "github.com/xz1220/github-radar/internal/store"
)

type Match struct {
	Slug       string  `json:"slug"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

type Store interface {
	GetTopicBySlug(context.Context, string) (domain.Topic, error)
	AssignRepositoryTopic(context.Context, domain.RepositoryTopic) (domain.TopicAssignmentResult, error)
}

var wordAI = regexp.MustCompile(`\b(ai|llm|llms|agentic|gpt|claude|codex)\b`)
var resourceWords = regexp.MustCompile(`\b(awesome|interview|quizzes|quiz|question-bank|cheatsheet)\b`)

// Infer is pure and safe for offline use. A repository can belong to several
// categories when its metadata explicitly describes several capabilities.
func Infer(fullName, description string, rawTopics []string) []Match {
	text := strings.ToLower(fullName + " " + description)
	topics := make(map[string]bool, len(rawTopics))
	for _, value := range rawTopics {
		topics[strings.ToLower(strings.TrimSpace(value))] = true
	}
	hasTopic := func(values ...string) bool {
		for _, value := range values {
			if topics[value] {
				return true
			}
		}
		return false
	}
	contains := func(values ...string) bool {
		for _, value := range values {
			if strings.Contains(text, value) {
				return true
			}
		}
		return false
	}
	// Learning collections are useful, but should not appear as agent products.
	if resourceWords.MatchString(text) || contains("curated list", "list of resources", "skill assessment", "skills assessment", "interview questions") {
		return nil
	}
	ai := wordAI.MatchString(text) || hasTopic("ai", "artificial-intelligence", "llm", "large-language-models", "ai-agent", "ai-agents", "llm-agent", "llm-agents", "agentic-ai", "agent-skills", "mcp", "model-context-protocol") || contains("coding agent", "coding assistant", "software engineering agent", "人工智能", "智能体", "大语言模型")
	matches := map[string]Match{}
	add := func(slug, reason string, confidence float64) {
		if existing, ok := matches[slug]; ok && existing.Confidence >= confidence {
			return
		}
		matches[slug] = Match{slug, reason, confidence}
	}
	if curated, ok := curatedRepositories[strings.ToLower(strings.TrimSpace(fullName))]; ok {
		for _, slug := range curated.Slugs {
			add(slug, curated.Reason+" ("+curated.EvidenceURL+")", .98)
		}
	}
	// A workspace that runs other coding agents is a platform, not itself a
	// coding agent. This distinction matters for products such as Orca.
	agentHost := ai && contains("fleet of parallel agents", "fleet of agents", "parallel agents", "coding agent manager", "manage coding agents", "run any coding agent")
	if !agentHost && (hasTopic("coding-agent", "coding-agents", "ai-coding", "ai-coding-assistant") || (ai && contains("coding agent", "coding assistant", "code assistant", "pair programming", "software engineering agent", "编程助手", "编程智能体"))) {
		add("coding-agents", "Explicit AI coding assistant capability", .9)
	}
	if hasTopic("deep-research", "research-agent", "research-agents") || (ai && contains("research agent", "deep research", "deep-research", "autonomous research", "研究智能体", "深度研究")) {
		add("research-agents", "Explicit autonomous research capability", .9)
	}
	if hasTopic("computer-use", "browser-agent", "browser-use", "gui-agent") || (ai && contains("browser automation", "browser agent", "computer use", "computer-use", "desktop automation", "浏览器自动化", "电脑操作")) {
		add("browser-computer-agents", "AI browser or computer operation capability", .9)
	}
	if ai && (hasTopic("workflow-automation", "agent-workflow", "ai-automation") || contains("workflow automation", "business automation", "automate workflows", "workflow agent", "workflow builder", "工作流自动化", "业务自动化")) {
		add("workflow-agents", "AI workflow or business automation capability", .85)
	}
	if agentHost || (ai && (hasTopic("multi-agent", "multi-agent-system", "agent-framework", "agent-orchestration") || contains("agent framework", "framework for building ai agents", "framework for building agents", "multi-agent", "multi agent", "agent orchestration", "agent platform", "多智能体", "智能体框架"))) {
		add("agent-platforms", "Framework or coordination platform for AI agents", .85)
	}
	if ai && (hasTopic("multi-agent", "multi-agent-system") || contains("multi-agent", "multi agent", "parallel agents", "fleet of agents", "多智能体")) {
		add("multi-agent", "Explicit collaboration among AI agents", .9)
	}
	// Components need explicit AI context. Generic skills/SDK/browser libraries
	// remain unclassified instead of silently becoming AI products.
	if ai && (hasTopic("agent-skills", "claude-skills", "claude-code-skills") || contains("agent skills", "claude skills", "skills for claude", "skills for ai", "skill.md")) {
		add("skills", "Reusable skills explicitly intended for AI agents", .9)
		add("ai-infrastructure", "Reusable agent skill component", .9)
	}
	if ai && (hasTopic("agent-memory", "llm-memory") || contains("agent memory", "memory for ai", "memory for llm", "memory layer", "智能体记忆")) {
		add("memory", "Memory component explicitly intended for AI agents", .9)
		add("ai-infrastructure", "Agent memory infrastructure", .9)
	}
	if ai && (hasTopic("agent-harness", "llm-sandbox") || contains("agent harness", "agent runtime", "agent sandbox")) {
		add("harness", "AI agent execution component", .85)
		add("ai-infrastructure", "Agent runtime infrastructure", .85)
	}
	if ai && (hasTopic("mcp", "model-context-protocol") || contains("model context protocol", "mcp server")) {
		add("browser-and-tools", "Model Context Protocol tool integration", .9)
		add("ai-infrastructure", "Agent tool integration infrastructure", .9)
	}
	if ai && (hasTopic("llm-evaluation", "llm-observability", "ai-evaluation") || contains("agent evaluation", "llm observability", "llm evaluation", "agent observability")) {
		add("reliability", "AI evaluation or observability component", .9)
		add("ai-infrastructure", "AI reliability infrastructure", .9)
	}
	if hasTopic("llm-inference", "model-serving", "vector-database", "inference-engine") || (ai && contains("inference engine", "model serving", "llm inference", "vector database", "模型推理")) {
		add("ai-infrastructure", "Model serving, inference, or retrieval infrastructure", .85)
	}
	if len(matches) == 0 && (hasTopic("ai-agent", "ai-agents", "llm-agent", "llm-agents", "agentic-ai") || contains("ai agent", "ai-agent", "llm agent", "autonomous agent", "智能体")) {
		add("ai-agent", "Explicit AI agent; product type not established", .75)
	}
	result := make([]Match, 0, len(matches))
	for _, match := range matches {
		result = append(result, match)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Slug < result[j].Slug })
	return result
}

// Apply only adds unconfirmed assignments. The store protects both confirmed
// manual assignments and manual confidence=0 vetoes. Missing taxonomy entries
// are skipped, allowing installations with custom taxonomies to keep working.
func Apply(ctx context.Context, store Store, repository domain.Repository, rawTopics []string, now time.Time) (int, error) {
	changed := 0
	for _, match := range Infer(repository.FullName, repository.Description, rawTopics) {
		topic, err := store.GetTopicBySlug(ctx, match.Slug)
		if errors.Is(err, corestore.ErrNotFound) {
			continue
		}
		if err != nil {
			return changed, err
		}
		result, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
			RepositoryID: repository.GitHubRepoID, TopicID: topic.ID,
			Source: domain.TopicSourceAuto, Confidence: &match.Confidence, AssignedAt: now.UTC(),
		})
		if err != nil {
			return changed, fmt.Errorf("classify %s as %s: %w", repository.FullName, match.Slug, err)
		}
		if result.Changed {
			changed++
		}
	}
	return changed, nil
}
