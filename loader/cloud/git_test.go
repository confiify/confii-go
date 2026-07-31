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

func TestGitLoader_GitHub_URL(t *testing.T) {
	l := NewGit("https://github.com/owner/repo", "config.yaml", WithGitBranch("develop"))
	assert.Contains(t, l.Source(), "git:")
	assert.Contains(t, l.Source(), "develop")
}

func TestGitLoader_ResolveRawURL_GitHub(t *testing.T) {
	l := NewGit("https://github.com/owner/repo", "config/app.yaml",
		WithGitBranch("main"),
		WithGitToken("mytoken"),
	)

	rawURL, headers, err := l.resolveRawURL()
	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/owner/repo/main/config/app.yaml", rawURL)
	assert.Equal(t, "token mytoken", headers["Authorization"])
}

func TestGitLoader_ResolveRawURL_GitLab(t *testing.T) {
	l := NewGit("https://gitlab.com/group/project", "config.yaml",
		WithGitToken("gltoken"),
	)

	rawURL, headers, err := l.resolveRawURL()
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.com/group/project/-/raw/main/config.yaml", rawURL)
	assert.Equal(t, "gltoken", headers["PRIVATE-TOKEN"])
}

func TestGitLoader_ResolveRawURL_Unsupported(t *testing.T) {
	l := NewGit("https://bitbucket.org/owner/repo", "config.yaml")
	_, _, err := l.resolveRawURL()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// Load's transport and parsing path is hermetically exercised through
// loadResolved; provider raw-URL resolution and the composed Load error path
// are covered by the resolveRawURL and FullEndToEnd tests.
func TestGitLoader_LoadResolved_FetchesParsesAndSendsHeaders(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"app": {"name": "test-app"}}`))
	}))
	defer srv.Close()

	l := NewGit("https://github.com/owner/repo", "config.yaml")
	result, err := l.loadResolved(context.Background(),
		srv.URL+"/owner/repo/main/config.yaml",
		map[string]string{"Authorization": "token mytoken"})
	require.NoError(t, err)

	app, ok := result["app"].(map[string]any)
	require.True(t, ok, "parsed JSON must expose the nested app section")
	assert.Equal(t, "test-app", app["name"])
	assert.Equal(t, "/owner/repo/main/config.yaml", gotPath)
	assert.Equal(t, "token mytoken", gotAuth)
}
