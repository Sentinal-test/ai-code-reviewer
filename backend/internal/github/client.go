package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

func GetInstallationClient(ctx context.Context, appID int64, installationID int64, privateKeyBytes []byte) (*github.Client, error) {
	// 1. Generate JWT
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyBytes)
	if err != nil {
		return nil, err
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": time.Now().Add(-60 * time.Second).Unix(),
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iss": appID,
	})

	jwtString, err := token.SignedString(privateKey)
	if err != nil {
		return nil, err
	}

	// 2. Get Installation Token
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: jwtString})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	instToken, _, err := client.Apps.CreateInstallationToken(ctx, installationID, nil)
	if err != nil {
		return nil, err
	}

	// 3. Return client with installation token
	ts = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: instToken.GetToken()})
	tc = oauth2.NewClient(ctx, ts)
	return github.NewClient(tc), nil
}

func FetchPRDiff(ctx context.Context, client *github.Client, owner, repo string, prNumber int) (string, error) {
	diff, _, err := client.PullRequests.GetRaw(ctx, owner, repo, prNumber, github.RawOptions{Type: github.Diff})
	if err != nil {
		return "", err
	}
	return diff, nil
}

func PostComment(ctx context.Context, client *github.Client, owner, repo string, prNumber int, comment *github.PullRequestComment) error {
	_, _, err := client.PullRequests.CreateComment(ctx, owner, repo, prNumber, comment)
	return err
}

func PostGeneralComment(ctx context.Context, client *github.Client, owner, repo string, prNumber int, body string) error {
	comment := &github.IssueComment{
		Body: github.String(body),
	}
	_, _, err := client.Issues.CreateComment(ctx, owner, repo, prNumber, comment)
	return err
}

func CreateCheckRun(ctx context.Context, client *github.Client, owner, repo string, headSHA string) (int64, error) {
	opts := github.CreateCheckRunOptions{
		Name:      "AI Code Review",
		HeadSHA:   headSHA,
		Status:    github.String("in_progress"),
		StartedAt: &github.Timestamp{Time: time.Now()},
	}
	run, _, err := client.Checks.CreateCheckRun(ctx, owner, repo, opts)
	if err != nil {
		return 0, err
	}
	return run.GetID(), nil
}

func UpdateCheckRun(ctx context.Context, client *github.Client, owner, repo string, checkRunID int64, conclusion string, summary string) error {
	opts := github.UpdateCheckRunOptions{
		Name:        "AI Code Review",
		Status:      github.String("completed"),
		Conclusion:  github.String(conclusion),
		CompletedAt: &github.Timestamp{Time: time.Now()},
		Output: &github.CheckRunOutput{
			Title:   github.String("AI Code Review"),
			Summary: github.String(summary),
		},
	}
	_, _, err := client.Checks.UpdateCheckRun(ctx, owner, repo, checkRunID, opts)
	return err
}

func GetRepoTree(ctx context.Context, client *github.Client, owner, repo string, branch string) (string, error) {
	// Get the tree recursively
	// We need the SHA of the branch first.
	// Actually, we can pass the branch name as the SHA to GetTree if using the API directly,
	// but strictly we should resolve the ref.
	// client.Git.GetTree accepts a SHA.

	// Let's resolve the branch ref to get the SHA
	ref, _, err := client.Git.GetRef(ctx, owner, repo, "heads/"+branch)
	if err != nil {
		// If branch not found, maybe it's a SHA already?
		// Or try default.
		return "", fmt.Errorf("could not resolve branch %s: %v", branch, err)
	}
	sha := ref.GetObject().GetSHA()

	tree, _, err := client.Git.GetTree(ctx, owner, repo, sha, true) // true for recursive
	if err != nil {
		return "", err
	}

	var fileList []string
	for _, entry := range tree.Entries {
		path := entry.GetPath()
		// Filter unnecessary parts
		if shouldIgnore(path) {
			continue
		}
		// We only want files for structure context usually, or maybe dirs too?
		// Let's include everything that isn't ignored to show structure.
		fileList = append(fileList, path)
	}

	return strings.Join(fileList, "\n"), nil
}

func shouldIgnore(path string) bool {
	// Simple ignore list
	ignoredPrefixes := []string{
		".git/", ".github/", ".idea/", ".vscode/",
		"node_modules/", "vendor/", "dist/", "build/",
		"coverage/", "test/", "__tests__/",
	}
	ignoredSuffixes := []string{
		".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico",
		".pdf", ".zip", ".tar", ".gz", ".exe", ".dll", ".so", ".dylib",
		".lock", "go.sum", "yarn.lock", "package-lock.json",
	}

	for _, p := range ignoredPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	for _, s := range ignoredSuffixes {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	// Ignore hidden files (starting with .)
	if strings.HasPrefix(path, ".") {
		return true // simple check covering .gitignore etc.
	}

	return false
}

func GetFileContent(ctx context.Context, client *github.Client, owner, repo, path, ref string) (string, error) {
	fc, _, _, err := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return "", err
	}

	if fc == nil {
		return "", fmt.Errorf("file content is nil")
	}

	content, err := fc.GetContent()
	if err != nil {
		return "", err
	}

	return content, nil
}

func GetChangedFilesContent(ctx context.Context, client *github.Client, owner, repo string, prNumber int, ref string) (map[string]string, error) {
	files, _, err := client.PullRequests.ListFiles(ctx, owner, repo, prNumber, &github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, err
	}

	contentMap := make(map[string]string)

	for _, f := range files {
		path := f.GetFilename()
		if shouldIgnore(path) {
			continue
		}
		if f.GetStatus() == "deleted" {
			continue
		}

		content, err := GetFileContent(ctx, client, owner, repo, path, ref)
		if err != nil {
			fmt.Printf("⚠️ Failed to fetch content for %s: %v\n", path, err)
			continue
		}
		contentMap[path] = content
	}
	return contentMap, nil
}
