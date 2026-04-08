package action

import (
	"bytes"
	"code-review/backend/internal/memory"
	"code-review/backend/internal/models"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GitLabClient struct {
	client    *http.Client
	baseURL   string
	token     string
	projectID string
	repoPath  string
}

type gitLabMergeRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type gitLabCommit struct {
	Message string `json:"message"`
	Title   string `json:"title"`
}

type gitLabPosition struct {
	NewPath string `json:"new_path"`
	OldPath string `json:"old_path"`
	NewLine *int   `json:"new_line"`
	OldLine *int   `json:"old_line"`
}

type gitLabDiscussionNote struct {
	ID         int64             `json:"id"`
	Body       string            `json:"body"`
	Resolvable bool              `json:"resolvable"`
	Resolved   bool              `json:"resolved"`
	Position   *gitLabPosition   `json:"position"`
}

type gitLabDiscussion struct {
	ID             string               `json:"id"`
	IndividualNote bool                 `json:"individual_note"`
	Notes          []gitLabDiscussionNote `json:"notes"`
	Resolved       bool                 `json:"resolved"`
}

type gitLabMRNote struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

type gitLabMRVersion struct {
	ID            int64  `json:"id"`
	HeadCommitSHA string `json:"head_commit_sha"`
	BaseCommitSHA string `json:"base_commit_sha"`
	StartCommitSHA string `json:"start_commit_sha"`
}

func NewGitLabClient(token, baseURL, projectID, repoPath string) *GitLabClient {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = "https://gitlab.com/api/v4"
	}
	return &GitLabClient{
		client: &http.Client{Timeout: 30 * time.Second},
		baseURL: baseURL,
		token: token,
		projectID: projectID,
		repoPath: repoPath,
	}
}

func (g *GitLabClient) projectRef() string {
	ref := strings.TrimSpace(g.projectID)
	if ref == "" {
		ref = strings.TrimSpace(g.repoPath)
	}
	return url.PathEscape(ref)
}

func (g *GitLabClient) GetMergeRequest(ctx context.Context, mrIID int) (*models.PRContext, error) {
	project := g.projectRef()
	if project == "" {
		return nil, fmt.Errorf("gitlab project id/path is required")
	}

	var mr gitLabMergeRequest
	if err := g.doJSON(ctx, http.MethodGet, fmt.Sprintf("/projects/%s/merge_requests/%d", project, mrIID), nil, nil, &mr); err != nil {
		return nil, err
	}

	var commits []gitLabCommit
	if err := g.doJSON(ctx, http.MethodGet, fmt.Sprintf("/projects/%s/merge_requests/%d/commits", project, mrIID), nil, nil, &commits); err != nil {
		fmt.Printf("⚠️ Failed to fetch GitLab MR commits for !%d: %v\n", mrIID, err)
	}

	var messages []string
	if len(commits) > 0 {
		start := 0
		if len(commits) > 5 {
			start = len(commits) - 5
		}
		for _, commit := range commits[start:] {
			msg := strings.TrimSpace(commit.Message)
			if msg == "" {
				msg = strings.TrimSpace(commit.Title)
			}
			if msg != "" {
				messages = append(messages, msg)
			}
		}
	}

	return &models.PRContext{
		Title:          mr.Title,
		Body:           mr.Description,
		CommitMessages: messages,
	}, nil
}

func (g *GitLabClient) FetchPreviousFindings(ctx context.Context, mrIID int) ([]models.PreviousFinding, error) {
	project := g.projectRef()
	if project == "" {
		return nil, fmt.Errorf("gitlab project id/path is required")
	}

	var findings []models.PreviousFinding

	var discussions []gitLabDiscussion
	if err := g.doPaginatedJSON(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/discussions", project, mrIID), &discussions); err != nil {
		return nil, fmt.Errorf("listing gitlab discussions: %w", err)
	}
	for _, discussion := range discussions {
		for _, note := range discussion.Notes {
			file := ""
			line := 0
			if note.Position != nil {
				file = note.Position.NewPath
				if file == "" {
					file = note.Position.OldPath
				}
				if note.Position.NewLine != nil {
					line = *note.Position.NewLine
				} else if note.Position.OldLine != nil {
					line = *note.Position.OldLine
				}
			}
			if pf := memory.ParseMarker(note.Body, file, line, note.ID); pf != nil {
				pf.ThreadID = discussion.ID
				findings = append(findings, *pf)
			}
		}
	}

	var notes []gitLabMRNote
	if err := g.doPaginatedJSON(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/notes", project, mrIID), &notes); err != nil {
		return nil, fmt.Errorf("listing gitlab notes: %w", err)
	}
	for _, note := range notes {
		if pf := memory.ParseMarker(note.Body, "", 0, note.ID); pf != nil {
			findings = append(findings, *pf)
		}
	}

	sortFindingsForStability(findings)
	return findings, nil
}

func (g *GitLabClient) PostReview(ctx context.Context, mrIID int, result *models.ReviewResult, commitSHA string, diff string, previous []models.PreviousFinding) error {
	if result == nil {
		return nil
	}

	project := g.projectRef()
	if project == "" {
		return fmt.Errorf("gitlab project id/path is required")
	}

	validLines := extractValidDiffLines(diff)
	version, versionErr := g.getLatestMRVersion(ctx, mrIID)
	if versionErr != nil {
		fmt.Printf("⚠️ Failed to load latest GitLab MR version for !%d: %v. Inline comments will fall back to general notes.\n", mrIID, versionErr)
	}

	successCount := 0
	failedInline := 0
	for i, c := range result.Comments {
		// Rate-limit protection: brief pause between posts, longer pause every 5 comments
		if i > 0 {
			time.Sleep(200 * time.Millisecond)
			if i%5 == 0 {
				time.Sleep(1 * time.Second)
			}
		}

		body := buildMarkerCommentBody(c)
		if c.Line <= 0 {
			fallbackMsg := fmt.Sprintf("⚠️ **Review comment for %s** (could not resolve line number)\n\n%s", c.File, body)
			if err := g.postGeneralNote(ctx, mrIID, fallbackMsg); err == nil {
				successCount++
			}
			continue
		}

		snappedLine := c.Line
		if fileLines, ok := validLines[c.File]; ok {
			snappedLine = snapToValidLine(c.Line, fileLines)
			if snappedLine == 0 {
				fallbackMsg := fmt.Sprintf("⚠️ **Review comment for %s:L%d** (line not in diff)\n\n%s", c.File, c.Line, body)
				if err := g.postGeneralNote(ctx, mrIID, fallbackMsg); err == nil {
					successCount++
				}
				continue
			}
		}

		if version != nil {
			if err := g.postInlineDiscussion(ctx, mrIID, version, c.File, snappedLine, body); err == nil {
				successCount++
				continue
			} else {
				fmt.Printf("  ⚠️ Failed to post GitLab inline discussion on %s:L%d: %v\n", c.File, snappedLine, err)
				failedInline++
			}
		}

		fallbackMsg := fmt.Sprintf("⚠️ **Could not post inline on %s:L%d**\n\n%s", c.File, c.Line, body)
		if err := g.postGeneralNote(ctx, mrIID, fallbackMsg); err == nil {
			successCount++
		}
	}

	resBlock := buildResolutionsBlock(result.Resolutions)
	summaryMsg := fmt.Sprintf("### AI Code Review Summary\n\n%s%s", result.Summary, resBlock)
	if failedInline > 0 {
		summaryMsg += fmt.Sprintf("\n\n---\n*Note: %d comments were posted as general notes because their line numbers could not be resolved in the merge request diff.*", failedInline)
	}
	if err := g.postGeneralNote(ctx, mrIID, summaryMsg); err != nil {
		return fmt.Errorf("posting gitlab summary note: %w", err)
	}

	g.ResolveDiscussions(ctx, mrIID, result.Resolutions, previous)
	fmt.Printf("✅ GitLab Review Complete | Posted %d/%d comments successfully\n", successCount, len(result.Comments))
	return nil
}

func (g *GitLabClient) ResolveDiscussions(ctx context.Context, mrIID int, resolutions []models.Resolution, previous []models.PreviousFinding) {
	if len(resolutions) == 0 || len(previous) == 0 {
		return
	}

	byCommentID := make(map[int64]models.PreviousFinding, len(previous))
	for _, finding := range previous {
		if int64(finding.CommentID) > 0 && finding.ThreadID != "" {
			byCommentID[int64(finding.CommentID)] = finding
		}
	}
	if len(byCommentID) == 0 {
		return
	}

	seenThreads := map[string]struct{}{}
	resolvedCount := 0
	for _, res := range resolutions {
		if !strings.EqualFold(res.Status, "resolved") {
			continue
		}

		finding, ok := byCommentID[int64(res.CommentID)]
		if !ok || finding.ThreadID == "" {
			continue
		}
		if _, seen := seenThreads[finding.ThreadID]; seen {
			continue
		}
		if err := g.setDiscussionResolved(ctx, mrIID, finding.ThreadID, true); err != nil {
			fmt.Printf("  ⚠️ Failed to resolve GitLab discussion %s: %v\n", finding.ThreadID, err)
			continue
		}
		seenThreads[finding.ThreadID] = struct{}{}
		resolvedCount++
	}

	if resolvedCount > 0 {
		fmt.Printf("  🎯 [Resolve] Resolved %d GitLab discussions\n", resolvedCount)
	}
}

func (g *GitLabClient) getLatestMRVersion(ctx context.Context, mrIID int) (*gitLabMRVersion, error) {
	var versions []gitLabMRVersion
	if err := g.doJSON(ctx, http.MethodGet, fmt.Sprintf("/projects/%s/merge_requests/%d/versions", g.projectRef(), mrIID), nil, nil, &versions); err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("no merge request versions returned")
	}
	return &versions[0], nil
}

func (g *GitLabClient) postInlineDiscussion(ctx context.Context, mrIID int, version *gitLabMRVersion, path string, line int, body string) error {
	form := url.Values{}
	form.Set("body", body)
	form.Set("position[position_type]", "text")
	form.Set("position[base_sha]", version.BaseCommitSHA)
	form.Set("position[head_sha]", version.HeadCommitSHA)
	form.Set("position[start_sha]", version.StartCommitSHA)
	form.Set("position[new_path]", path)
	form.Set("position[old_path]", path)
	form.Set("position[new_line]", strconv.Itoa(line))

	return g.doJSON(ctx, http.MethodPost, fmt.Sprintf("/projects/%s/merge_requests/%d/discussions", g.projectRef(), mrIID), form, nil, nil)
}

func (g *GitLabClient) postGeneralNote(ctx context.Context, mrIID int, body string) error {
	form := url.Values{}
	form.Set("body", body)
	return g.doJSON(ctx, http.MethodPost, fmt.Sprintf("/projects/%s/merge_requests/%d/notes", g.projectRef(), mrIID), form, nil, nil)
}

func (g *GitLabClient) setDiscussionResolved(ctx context.Context, mrIID int, discussionID string, resolved bool) error {
	form := url.Values{}
	form.Set("resolved", strconv.FormatBool(resolved))
	return g.doJSON(ctx, http.MethodPut, fmt.Sprintf("/projects/%s/merge_requests/%d/discussions/%s", g.projectRef(), mrIID, url.PathEscape(discussionID)), form, nil, nil)
}

func buildMarkerCommentBody(c models.ReviewComment) string {
	encodedFile := base64.RawURLEncoding.EncodeToString([]byte(c.File))
	marker := fmt.Sprintf("<!-- ai-reviewer:v1 file=%s line=%d severity=%s layer=%s hash=%s -->",
		encodedFile, c.Line, c.Severity, c.Layer, memory.Fingerprint(c.File, c.Layer, c.Message))
	return marker + "\n" + fmt.Sprintf("**[%s]** %s\n\n%s", strings.ToUpper(c.Severity), c.Layer, c.Message)
}

func (g *GitLabClient) doPaginatedJSON(ctx context.Context, path string, dest interface{}) error {
	switch out := dest.(type) {
	case *[]gitLabDiscussion:
		var all []gitLabDiscussion
		if err := g.doPaginated(ctx, path, func(resp []byte) error {
			var page []gitLabDiscussion
			if err := json.Unmarshal(resp, &page); err != nil {
				return err
			}
			all = append(all, page...)
			return nil
		}); err != nil {
			return err
		}
		*out = all
		return nil
	case *[]gitLabMRNote:
		var all []gitLabMRNote
		if err := g.doPaginated(ctx, path, func(resp []byte) error {
			var page []gitLabMRNote
			if err := json.Unmarshal(resp, &page); err != nil {
				return err
			}
			all = append(all, page...)
			return nil
		}); err != nil {
			return err
		}
		*out = all
		return nil
	default:
		return fmt.Errorf("unsupported paginated destination type %T", dest)
	}
}

func (g *GitLabClient) doPaginated(ctx context.Context, path string, consume func([]byte) error) error {
	page := 1
	for {
		query := url.Values{}
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))
		body, headers, err := g.doRaw(ctx, http.MethodGet, path, nil, query)
		if err != nil {
			return err
		}
		if err := consume(body); err != nil {
			return err
		}
		next := strings.TrimSpace(headers.Get("X-Next-Page"))
		if next == "" {
			return nil
		}
		page, err = strconv.Atoi(next)
		if err != nil {
			return nil
		}
	}
}

func (g *GitLabClient) doJSON(ctx context.Context, method, path string, form url.Values, query url.Values, dest interface{}) error {
	body, _, err := g.doRaw(ctx, method, path, form, query)
	if err != nil {
		return err
	}
	if dest == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dest)
}

func (g *GitLabClient) doRaw(ctx context.Context, method, path string, form url.Values, query url.Values) ([]byte, http.Header, error) {
	endpoint := g.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", g.token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, fmt.Errorf("gitlab api %s %s failed (HTTP %d): %s", method, path, resp.StatusCode, string(bytes.TrimSpace(respBody)))
	}
	return respBody, resp.Header, nil
}

func sortFindingsForStability(findings []models.PreviousFinding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Hash < findings[j].Hash
	})
}
