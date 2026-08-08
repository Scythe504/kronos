package builder

import (
	"context"
	"fmt"
	"net/url"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

// formatAuthenticatedURL embeds username/token credentials into HTTPS repo URLs
func formatAuthenticatedURL(rawURL string, username string, token string) string {
	if token == "" && username == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return rawURL
	}
	if username != "" && token != "" {
		parsed.User = url.UserPassword(username, token)
	} else if token != "" {
		parsed.User = url.User(token)
	} else if username != "" {
		parsed.User = url.User(username)
	}
	return parsed.String()
}

// GetRepoRef returns default branch or validates git URL
func GetRepoRef(ctx context.Context, repoURL string) (string, error) {
	if repoURL == "" {
		return "", fmt.Errorf("repo URL is empty")
	}
	return "main", nil
}

func FetchRemoteHeadCommit(ctx context.Context, repoURL string, repoRef string) string {
	remStore := memory.NewStorage()
	remConfig := &config.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
	}
	remote := git.NewRemote(remStore, remConfig)
	refs, err := remote.ListContext(ctx, &git.ListOptions{})
	if err != nil {
		return ""
	}
	targetRef := repoRef
	if targetRef == "" {
		targetRef = "refs/heads/main"
	}
	for _, ref := range refs {
		if ref.Name().String() == targetRef || ref.Name().Short() == targetRef || (repoRef == "" && ref.Name() == plumbing.HEAD) {
			return ref.Hash().String()
		}
	}
	if len(refs) > 0 {
		return refs[0].Hash().String()
	}
	return ""
}

func CloneRepo(ctx context.Context, repoURL string, repoRef string, targetDir string) error {
	cloneOpts := &git.CloneOptions{
		URL:   repoURL,
		Depth: 1,
	}
	if repoRef != "" {
		cloneOpts.ReferenceName = plumbing.ReferenceName(repoRef)
	}

	_, err := git.PlainCloneContext(ctx, targetDir, cloneOpts)
	if err != nil {
		fallbackOpts := &git.CloneOptions{
			URL: repoURL,
		}
		_, fallbackErr := git.PlainCloneContext(ctx, targetDir, fallbackOpts)
		if fallbackErr != nil {
			return fmt.Errorf("failed to clone %s into %s: %w", repoURL, targetDir, fallbackErr)
		}
	}

	return nil
}
