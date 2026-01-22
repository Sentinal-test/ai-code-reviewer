package github

import (
	"context"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

func GetInstallationClient(ctx context.Context, appID int64, installationID int64, privateKeyPath string) (*github.Client, error) {
	// 1. Generate JWT
	privateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, err
	}
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
