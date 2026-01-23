package models

type User struct {
	ID                int
	GitHubID          string
	LLMAPIKey         string
	GitHubAccessToken string
}

type RepoSettings struct {
	ID                  int
	RepoID              string
	UserID              int
	IsActive            bool
	SecurityEnabled     bool
	BugEnabled          bool
	LintEnabled         bool
	PerformanceEnabled  bool
	ArchitectureEnabled bool
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
