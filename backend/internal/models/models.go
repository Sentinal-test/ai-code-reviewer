package models

type User struct {
	ID                int
	GitHubID          string
	LLMAPIKey         string
	GitHubAccessToken string
}

type RepoSettings struct {
	ID                  int    `json:"id"`
	RepoID              string `json:"repo_id"`
	UserID              int    `json:"user_id"`
	IsActive            bool   `json:"is_active"`
	SecurityEnabled     bool   `json:"security_enabled"`
	BugEnabled          bool   `json:"bug_enabled"`
	LintEnabled         bool   `json:"lint_enabled"`
	PerformanceEnabled  bool   `json:"performance_enabled"`
	ArchitectureEnabled bool   `json:"architecture_enabled"`
}

type ReviewComment struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Layer    string `json:"layer"`
	Message  string `json:"message"`
}

type ReviewResult struct {
	Summary  string          `json:"summary"`
	Comments []ReviewComment `json:"comments"`
}

// PRContext holds metadata about a PR for enhanced LLM context
type PRContext struct {
	Title          string
	Body           string
	CommitMessages []string
}

// DeveloperRules holds custom review rules defined by repo maintainers.
type DeveloperRules struct {
	Instructions []string `yaml:"instructions"`
	Ignore       []string `yaml:"ignore"`
	Focus        []string `yaml:"focus"`
}
