// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cloud

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
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
	repository, err := url.Parse(l.repoURL)
	if err != nil {
		return "", nil, fmt.Errorf("parse repository URL: %w", err)
	}
	if repository.Scheme != "https" || repository.User != nil || repository.RawQuery != "" || repository.Fragment != "" {
		return "", nil, fmt.Errorf("repository URL must be an HTTPS URL without credentials, query, or fragment")
	}
	if repository.Host != repository.Hostname() {
		return "", nil, fmt.Errorf("repository URL must not specify a custom port")
	}
	repositoryPath := strings.Trim(strings.TrimSuffix(repository.Path, ".git"), "/")
	if err := validateGitPath(repositoryPath, "repository"); err != nil {
		return "", nil, err
	}
	if err := validateGitPath(l.branch, "branch"); err != nil {
		return "", nil, err
	}
	if err := validateGitPath(l.filePath, "file"); err != nil {
		return "", nil, err
	}

	switch strings.ToLower(repository.Hostname()) {
	case "github.com":
		if len(strings.Split(repositoryPath, "/")) != 2 {
			return "", nil, fmt.Errorf("GitHub repository URL must contain exactly owner and repository")
		}
		raw := &url.URL{Scheme: "https", Host: "raw.githubusercontent.com", Path: path.Join(repositoryPath, l.branch, l.filePath)}
		if l.token != "" {
			headers["Authorization"] = "token " + l.token
		}
		return raw.String(), headers, nil

	case "gitlab.com":
		if len(strings.Split(repositoryPath, "/")) < 2 {
			return "", nil, fmt.Errorf("GitLab repository URL must contain a namespace and repository")
		}
		raw := &url.URL{Scheme: "https", Host: "gitlab.com", Path: path.Join(repositoryPath, "-", "raw", l.branch, l.filePath)}
		if l.token != "" {
			headers["PRIVATE-TOKEN"] = l.token
		}
		return raw.String(), headers, nil

	default:
		return "", nil, fmt.Errorf("unsupported git provider host %q (only github.com and gitlab.com are supported)", repository.Hostname())
	}
}

func validateGitPath(value string, name string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") {
		return fmt.Errorf("%s path must be non-empty and relative", name)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("%s path contains an invalid segment", name)
		}
	}
	return nil
}
