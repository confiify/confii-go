package cloud

import (
	"context"
	"fmt"
	"os"
	"strings"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
)

// GitLoader loads configuration from a file in a Git repository via raw content URLs.
// Supports GitHub and GitLab.
type GitLoader struct {
	repoURL  string
	filePath string
	branch   string
	token    string
}

// GitOption configures a GitLoader.
type GitOption func(*GitLoader)

// WithGitBranch sets the branch (default "main").
func WithGitBranch(branch string) GitOption {
	return func(l *GitLoader) { l.branch = branch }
}

// WithGitToken sets the access token for private repos.
func WithGitToken(token string) GitOption {
	return func(l *GitLoader) { l.token = token }
}

// NewGit creates a new Git loader.
func NewGit(repoURL, filePath string, opts ...GitOption) *GitLoader {
	l := &GitLoader{
		repoURL:  repoURL,
		filePath: filePath,
		branch:   "main",
		token:    os.Getenv("GIT_TOKEN"),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func (l *GitLoader) Source() string {
	return fmt.Sprintf("git:%s@%s/%s", l.repoURL, l.branch, l.filePath)
}

func (l *GitLoader) Load(ctx context.Context) (map[string]any, error) {
	rawURL, headers, err := l.resolveRawURL()
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	var httpOpts []loader.HTTPOption
	if len(headers) > 0 {
		httpOpts = append(httpOpts, loader.WithHeaders(headers))
	}

	httpLoader := loader.NewHTTP(rawURL, httpOpts...)
	return httpLoader.Load(ctx)
}

func (l *GitLoader) resolveRawURL() (string, map[string]string, error) {
	headers := make(map[string]string)
	repoURL := strings.TrimSuffix(l.repoURL, ".git")

	switch {
	case strings.Contains(repoURL, "github.com"):
		// https://github.com/{owner}/{repo} → https://raw.githubusercontent.com/{owner}/{repo}/{branch}/{path}
		path := strings.TrimPrefix(repoURL, "https://github.com/")
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", path, l.branch, l.filePath)
		if l.token != "" {
			headers["Authorization"] = "token " + l.token
		}
		return rawURL, headers, nil

	case strings.Contains(repoURL, "gitlab.com"):
		// https://gitlab.com/{path} → https://gitlab.com/{path}/-/raw/{branch}/{file_path}
		rawURL := fmt.Sprintf("%s/-/raw/%s/%s", repoURL, l.branch, l.filePath)
		if l.token != "" {
			headers["PRIVATE-TOKEN"] = l.token
		}
		return rawURL, headers, nil

	default:
		return "", nil, fmt.Errorf("unsupported git provider: %s (only GitHub and GitLab are supported)", repoURL)
	}
}
