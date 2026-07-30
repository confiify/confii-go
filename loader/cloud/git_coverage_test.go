// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitLoader_DefaultBranch(t *testing.T) {
	l := NewGit("https://github.com/owner/repo", "config.yaml")
	assert.Equal(t, "main", l.branch)
}

func TestGitLoader_WithBranch(t *testing.T) {
	l := NewGit("https://github.com/owner/repo", "config.yaml", WithGitBranch("develop"))
	assert.Equal(t, "develop", l.branch)
}

func TestGitLoader_WithToken(t *testing.T) {
	l := NewGit("https://github.com/owner/repo", "config.yaml", WithGitToken("mytoken"))
	assert.Equal(t, "mytoken", l.token)
}

func TestGitLoader_Source(t *testing.T) {
	l := NewGit("https://github.com/owner/repo", "config.yaml", WithGitBranch("main"))
	source := l.Source()
	assert.Equal(t, "git:https://github.com/owner/repo@main/config.yaml", source)
}

func TestGitLoader_SourceCustomBranch(t *testing.T) {
	l := NewGit("https://gitlab.com/group/project", "app.json", WithGitBranch("release/v2"))
	source := l.Source()
	assert.Contains(t, source, "release/v2")
	assert.Contains(t, source, "app.json")
}

func TestGitLoader_ResolveRawURL_GitHubStripsGitSuffix(t *testing.T) {
	l := NewGit("https://github.com/owner/repo.git", "config.yaml", WithGitBranch("main"))

	rawURL, _, err := l.resolveRawURL()
	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/owner/repo/main/config.yaml", rawURL)
}

func TestGitLoader_ResolveRawURL_GitLabStripsGitSuffix(t *testing.T) {
	l := NewGit("https://gitlab.com/group/project.git", "settings.toml",
		WithGitBranch("feature/test"),
		WithGitToken("gl-token"),
	)

	rawURL, headers, err := l.resolveRawURL()
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.com/group/project/-/raw/feature/test/settings.toml", rawURL)
	assert.Equal(t, "gl-token", headers["PRIVATE-TOKEN"])
}

func TestGitLoader_ResolveRawURL_GitHubNoToken(t *testing.T) {
	l := NewGit("https://github.com/owner/repo", "config.yaml",
		WithGitToken(""),
	)

	_, headers, err := l.resolveRawURL()
	require.NoError(t, err)
	_, hasAuth := headers["Authorization"]
	assert.False(t, hasAuth)
}

func TestGitLoader_ResolveRawURL_GitLabNoToken(t *testing.T) {
	l := NewGit("https://gitlab.com/group/project", "config.yaml",
		WithGitToken(""),
	)

	_, headers, err := l.resolveRawURL()
	require.NoError(t, err)
	_, hasToken := headers["PRIVATE-TOKEN"]
	assert.False(t, hasToken)
}

func TestGitLoader_ResolveRawURL_UnsupportedBitbucket(t *testing.T) {
	l := NewGit("https://bitbucket.org/owner/repo", "config.yaml")
	_, _, err := l.resolveRawURL()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported git provider")
}

func TestGitLoader_ResolveRawURL_UnsupportedCustomDomain(t *testing.T) {
	l := NewGit("https://mygit.example.com/owner/repo", "config.yaml")
	_, _, err := l.resolveRawURL()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestGitLoader_ResolveRawURL_GitHubNestedPath(t *testing.T) {
	l := NewGit("https://github.com/org/repo", "path/to/config.yaml",
		WithGitBranch("main"),
	)

	rawURL, _, err := l.resolveRawURL()
	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/org/repo/main/path/to/config.yaml", rawURL)
}

func TestGitLoader_ResolveRawURL_GitLabSubgroup(t *testing.T) {
	l := NewGit("https://gitlab.com/org/subgroup/project", "config/app.yaml",
		WithGitBranch("develop"),
	)

	rawURL, _, err := l.resolveRawURL()
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.com/org/subgroup/project/-/raw/develop/config/app.yaml", rawURL)
}

func TestGitLoader_MultipleOptions(t *testing.T) {
	l := NewGit("https://github.com/owner/repo", "config.yaml",
		WithGitBranch("staging"),
		WithGitToken("tok123"),
	)
	assert.Equal(t, "staging", l.branch)
	assert.Equal(t, "tok123", l.token)
}

func TestGitLoader_Load_FullEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"database": {"host": "git-host", "port": 5432}}`))
	}))
	defer srv.Close()

	l := NewGit("https://bitbucket.org/owner/repo", "config.yaml")
	_, err := l.Load(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestGitLoader_Load_GitHubToken(t *testing.T) {
	l := NewGit("https://github.com/owner/repo", "config.yaml",
		WithGitBranch("main"),
		WithGitToken("test-token"),
	)

	rawURL, headers, err := l.resolveRawURL()
	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/owner/repo/main/config.yaml", rawURL)
	assert.Equal(t, "token test-token", headers["Authorization"])
}
