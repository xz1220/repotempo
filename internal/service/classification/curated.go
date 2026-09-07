package classification

// These exact owner/repository exceptions supplement sparse stored descriptions
// when a project's purpose has been checked in its own repository. This is not
// a name-keyword rule: forks, lookalikes, and other repositories by the owner do
// not inherit it. Review evidence when an entry changes purpose or ownership.
// All resulting assignments remain automatic and subject to manual vetoes.
type curatedRepository struct {
	Slugs       []string
	Reason      string
	EvidenceURL string
}

// Evidence checked 2026-09-07 against the listed repository's README/About.
var curatedRepositories = map[string]curatedRepository{
	"anomalyco/opencode": {
		Slugs:       []string{"coding-agents"},
		Reason:      "OpenCode identifies itself as an open-source coding agent",
		EvidenceURL: "https://github.com/anomalyco/opencode",
	},
	"langchain-ai/langgraph": {
		Slugs:       []string{"agent-platforms"},
		Reason:      "LangGraph provides the framework and runtime for durable stateful agents",
		EvidenceURL: "https://github.com/langchain-ai/langgraph",
	},
	"browser-use/browser-use": {
		Slugs:       []string{"browser-computer-agents"},
		Reason:      "Browser Use enables AI agents to operate websites and automate browser tasks",
		EvidenceURL: "https://github.com/browser-use/browser-use",
	},
	"stablyai/orca": {
		Slugs:       []string{"agent-platforms", "multi-agent", "workbench", "client-and-remote-access"},
		Reason:      "Orca is a workspace for parallel coding agents with desktop and remote interfaces",
		EvidenceURL: "https://github.com/stablyai/orca",
	},
	"deepseek-ai/deepseek-harness": {
		Slugs:       []string{"ai-infrastructure", "harness"},
		Reason:      "DeepSeek documents dsh as an extensible plugin-based agent harness",
		EvidenceURL: "https://github.com/deepseek-ai/deepseek-harness",
	},
}
