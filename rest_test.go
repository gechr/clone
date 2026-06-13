package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRESTLister returns a restRepoLister wired to a test server instead of
// the live GitHub API.
func testRESTLister(srv *httptest.Server) *restRepoLister {
	return &restRepoLister{client: srv.Client(), baseURL: srv.URL}
}

// ghRepo builds a REST repository object as GitHub would return it.
func ghRepo(
	name, lang, visibility string,
	stars int,
	archived, fork bool,
	topics ...string,
) map[string]any {
	if topics == nil {
		topics = []string{}
	}
	return map[string]any{
		"name":             name,
		"language":         lang,
		"visibility":       visibility,
		"stargazers_count": stars,
		"archived":         archived,
		"fork":             fork,
		"topics":           topics,
		"owner":            map[string]any{"login": "octo"},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	assert.NoError(t, json.NewEncoder(w).Encode(v))
}

func TestRESTListOwnerReposPagination(t *testing.T) {
	t.Parallel()

	// Page 1 returns a full page (restPerPage), forcing a second request;
	// page 2 returns fewer, terminating the loop.
	var sawPages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/octo/repos", r.URL.Path)
		page := r.URL.Query().Get("page")
		sawPages = append(sawPages, page)
		switch page {
		case "1":
			batch := make([]map[string]any, 0, restPerPage)
			for i := range restPerPage {
				batch = append(batch, ghRepo(string(rune('a'+i%26))+string(rune('0'+i/26)),
					"Go", "public", 1, false, false))
			}
			writeJSON(t, w, batch)
		case "2":
			writeJSON(t, w, []map[string]any{ghRepo("tail", "Go", "public", 1, false, false)})
		default:
			t.Errorf("unexpected page %q", page)
		}
	}))
	defer srv.Close()

	repos, err := testRESTLister(
		srv,
	).ListOwnerRepos(context.Background(), "octo", repoListOptions{})
	require.NoError(t, err)
	require.Len(t, repos, restPerPage+1)
	require.Equal(t, []string{"1", "2"}, sawPages, "should stop after the short page")
}

func TestRESTListOwnerReposNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(t, w, map[string]any{"message": "Not Found"})
	}))
	defer srv.Close()

	_, err := testRESTLister(srv).ListOwnerRepos(context.Background(), "ghost", repoListOptions{})
	require.EqualError(t, err, `could not find GitHub owner "ghost"`)
}

func TestRESTListOwnerReposRateLimited(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Ratelimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		writeJSON(t, w, map[string]any{"message": "API rate limit exceeded"})
	}))
	defer srv.Close()

	_, err := testRESTLister(srv).ListOwnerRepos(context.Background(), "octo", repoListOptions{})
	require.ErrorIs(t, err, errAnonRateLimit, "exhausted anonymous quota maps to errAnonRateLimit")
}

func TestRESTForbiddenNotRateLimited(t *testing.T) {
	t.Parallel()

	// A 403 with quota remaining is a permission error, not a rate limit, so
	// it must NOT be reported as errAnonRateLimit.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Ratelimit-Remaining", "42")
		w.WriteHeader(http.StatusForbidden)
		writeJSON(t, w, map[string]any{"message": "Resource not accessible"})
	}))
	defer srv.Close()

	_, err := testRESTLister(srv).ListOwnerRepos(context.Background(), "octo", repoListOptions{})
	require.Error(t, err)
	require.NotErrorIs(t, err, errAnonRateLimit)
	require.EqualError(
		t,
		err,
		"querying repositories for octo: GitHub API request failed: Resource not accessible (403 Forbidden)",
	)
}

func TestRESTResolvePR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		head      string
		base      string
		state     string
		wantState string
		wantCross bool
	}{
		{"open same-repo", "octo/repo", "octo/repo", "open", "OPEN", false},
		{"open cross-repo", "fork/repo", "octo/repo", "open", "OPEN", true},
		{"closed", "octo/repo", "octo/repo", "closed", "CLOSED", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/repos/octo/repo/pulls/7", r.URL.Path)
					writeJSON(t, w, map[string]any{
						"state": tc.state,
						"head": map[string]any{
							"ref":  "feature",
							"repo": map[string]any{"full_name": tc.head},
						},
						"base": map[string]any{"repo": map[string]any{"full_name": tc.base}},
					})
				}),
			)
			defer srv.Close()

			info, err := testRESTLister(srv).ResolvePR(context.Background(), "octo", "repo", 7)
			require.NoError(t, err)
			require.Equal(t, "feature", info.HeadRefName)
			require.Equal(t, tc.wantState, info.State, "REST state is upper-cased to match GraphQL")
			require.Equal(t, tc.wantCross, info.IsCrossRepository)
		})
	}
}

func TestRESTResolvePRNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(t, w, map[string]any{"message": "Not Found"})
	}))
	defer srv.Close()

	_, err := testRESTLister(srv).ResolvePR(context.Background(), "octo", "repo", 99)
	require.EqualError(t, err, `could not find PR #99 in repository "octo/repo"`)
}

func TestRESTListViewerReposRequiresAuth(t *testing.T) {
	t.Parallel()

	// No server: anonymous viewer listing must fail fast without a request.
	lister := &restRepoLister{client: http.DefaultClient, baseURL: "http://invalid.invalid"}
	_, err := lister.ListViewerRepos(context.Background(), viewerStarred, repoListOptions{})
	require.EqualError(
		t,
		err,
		"starred repositories require authentication - set CLONE_GITHUB_TOKEN or run 'gh auth login'",
	)
}

func TestRepoInfoFromREST(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo restRepo
		opts repoListOptions
		want bool
	}{
		{
			name: "plain repo passes with zero options",
			repo: restRepo{Name: "a", Visibility: "public"},
			want: true,
		},
		{
			name: "archived excluded by default",
			repo: restRepo{Name: "a", Archived: true},
			want: false,
		},
		{
			name: "archived kept when requested",
			repo: restRepo{Name: "a", Archived: true},
			opts: repoListOptions{IncludeArchived: true},
			want: true,
		},
		{
			name: "fork excluded by default",
			repo: restRepo{Name: "a", Fork: true},
			want: false,
		},
		{
			name: "language filter mismatch",
			repo: restRepo{Name: "a", Language: "Ruby"},
			opts: repoListOptions{Languages: []string{"Go"}},
			want: false,
		},
		{
			name: "stars below minimum",
			repo: restRepo{Name: "a", Stargazers: 10},
			opts: repoListOptions{Stars: rangeFilter{min: 1000}},
			want: false,
		},
		{
			name: "topic filter mismatch",
			repo: restRepo{Name: "a", Topics: []string{"cli"}},
			opts: repoListOptions{TopicFilters: [][]string{{"tui"}}},
			want: false,
		},
		{
			name: "visibility filter mismatch",
			repo: restRepo{Name: "a", Visibility: "public"},
			opts: repoListOptions{Visibility: "private"},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			info, ok := repoInfoFromREST(tc.repo, "octo", tc.opts)
			require.Equal(t, tc.want, ok)
			if ok {
				require.Equal(t, "octo", info.Owner, "owner falls back to the queried owner")
			}
		})
	}
}
