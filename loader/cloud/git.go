// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cloud

import (
	"context"
	"fmt"
	"os"
	"strings"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

// GitLoader loads one file from a GitHub or GitLab repository through the
// provider's raw-content endpoint. It does not clone the repository.
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

// WithGitToken sets the access token for private repositories. NewGit otherwise
// reads GIT_TOKEN. The token is sent only to recognized GitHub or GitLab raw
// endpoints and is omitted from Source.
func WithGitToken(token string) GitOption {
	return func(l *GitLoader) { l.token = token }
}

// NewGit creates a loader for filePath at the selected branch. The default
// branch is main. Provider and URL validation is deferred to Load.
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

// Source returns the identifier for this loader's configuration source.
func (l *GitLoader) Source() string {
	return fmt.Sprintf("git:%s@%s/%s", l.repoURL, l.branch, l.filePath)
}

// Load resolves the provider-specific raw URL and delegates transport, format
// detection, context cancellation, and error classification to loader.HTTP.
// Unsupported providers return a load error.
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
