package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testDefaultOwner = "octo"

type fakeRepoLister struct {
	repos map[string][]repoInfo
	prs   map[string]prInfo
}

func (f fakeRepoLister) ListOwnerRepos(owner string, opts repoListOptions) ([]repoInfo, error) {
	var filtered []repoInfo
	for _, repo := range f.repos[owner] {
		if opts.Visibility != "" && opts.Visibility != "all" && repo.Visibility != opts.Visibility {
			continue
		}
		if len(opts.Languages) > 0 && !matchesAnyFold(opts.Languages, repo.Language) {
			continue
		}
		if len(opts.TopicFilters) > 0 && !matchesTopicFilters(opts.TopicFilters, repo.Topics...) {
			continue
		}
		filtered = append(filtered, repo)
	}
	return filtered, nil
}

func (f fakeRepoLister) ResolvePR(owner, repo string, number int) (prInfo, error) {
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	if info, ok := f.prs[key]; ok {
		return info, nil
	}
	return prInfo{}, fmt.Errorf("PR not found: %s", key)
}

func TestParseRepoRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  repoRequest
	}{
		{
			input: "repo",
			want:  repoRequest{Owner: testDefaultOwner, Name: "repo"},
		},
		{
			input: "owner/repo",
			want:  repoRequest{ExplicitOwner: true, Owner: "owner", Name: "repo"},
		},
		{
			input: "owner/repo=worktree",
			want: repoRequest{
				ExplicitOwner: true,
				Owner:         "owner",
				Name:          "repo",
				Dir:           "worktree",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()

			got, err := parseRepoRequest(test.input, testDefaultOwner)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestParseRepoRequestPRShorthand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  repoRequest
	}{
		{
			input: "repo#42",
			want:  repoRequest{Owner: testDefaultOwner, Name: "repo", PullRequest: "42"},
		},
		{
			input: "owner/repo#21",
			want: repoRequest{
				ExplicitOwner: true,
				Owner:         "owner",
				Name:          "repo",
				PullRequest:   "21",
			},
		},
		{
			input: "owner/repo#21=worktree",
			want: repoRequest{
				ExplicitOwner: true,
				Owner:         "owner",
				Name:          "repo",
				PullRequest:   "21",
				Dir:           "worktree",
			},
		},
		{
			input: "https://github.com/owner/repo/pull/21",
			want: repoRequest{
				ExplicitOwner: true,
				Owner:         "owner",
				Name:          "repo",
				PullRequest:   "21",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			input: "github.com/owner/repo#21",
			want: repoRequest{
				ExplicitOwner: true,
				Owner:         "owner",
				Name:          "repo",
				PullRequest:   "21",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			input: "git@github.com:owner/repo.git",
			want: repoRequest{
				ExplicitOwner: true,
				Owner:         "owner",
				Name:          "repo",
				Source:        "git@github.com:owner/repo.git",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()

			got, err := parseRepoRequest(test.input, testDefaultOwner)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestParseRepoRequestRejectsOverlongRepoName(t *testing.T) {
	t.Parallel()

	name := strings.Repeat("a", maxRepoNameBytes+1)

	_, err := parseRepoRequest("owner/"+name, testDefaultOwner)
	require.EqualError(t, err, fmt.Sprintf("invalid repository %q", "owner/"+name))
}

func TestResolveCloneTargetsPR(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:      testDefaultOwner,
		Repos:      []string{"owner/repo#21"},
		Method:     methodSSH,
		VCS:        vcsGit,
		Visibility: "all",
	}

	targets, _, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{
		prs: map[string]prInfo{
			"owner/repo#21": {
				HeadRefName:       "feature-branch",
				IsCrossRepository: false,
				State:             "OPEN",
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, "feature-branch", targets[0].Branch)
	require.Empty(t, targets[0].PullRequest)
}

func TestResolveCloneTargetsPRBranchConflict(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:      testDefaultOwner,
		Repos:      []string{"owner/repo#21"},
		Branch:     "main",
		Method:     methodSSH,
		VCS:        vcsGit,
		Visibility: "all",
	}

	_, _, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{})
	require.Error(t, err)
}

func TestResolveCloneTargetsRejectsNonPositivePRNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo string
		pr   string
	}{
		{name: "zero", repo: "owner/repo#0", pr: "0"},
		{name: "negative", repo: "owner/repo#-1", pr: "-1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cli := &CLI{
				Owner:      testDefaultOwner,
				Repos:      []string{test.repo},
				Method:     methodSSH,
				VCS:        vcsGit,
				Visibility: "all",
			}

			_, _, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{})
			require.EqualError(t, err, fmt.Sprintf("invalid PR number %q for owner/repo", test.pr))
		})
	}
}

func TestResolveCloneTargetsAllAndFilters(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:  testDefaultOwner,
		Repos:  []string{"all"},
		Method: methodSSH,
		VCS:    vcsGit,
		Topics: []string{"go"},
		TopicFilters: [][]string{
			{"go"},
		},
		Visibility: "all",
	}

	targets, baseDir, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{
		repos: map[string][]repoInfo{
			testDefaultOwner: {
				{Owner: testDefaultOwner, Name: "alpha", Topics: []string{"go"}},
				{Owner: testDefaultOwner, Name: "beta", Topics: []string{"rust"}},
			},
		},
	})
	require.NoError(t, err)
	require.Empty(t, baseDir)
	require.Len(t, targets, 1)
	require.Equal(t, testDefaultOwner+"/alpha", targets[0].Slug)
	require.Equal(t, "git@github.com:"+testDefaultOwner+"/alpha.git", targets[0].Source)
}

func TestResolveCloneTargetsImplicitAllForTopicFilters(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:  testDefaultOwner,
		Method: methodSSH,
		VCS:    vcsGit,
		Topics: []string{"go"},
		TopicFilters: [][]string{
			{"go"},
		},
		Visibility: "all",
	}

	targets, baseDir, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{
		repos: map[string][]repoInfo{
			testDefaultOwner: {
				{Owner: testDefaultOwner, Name: "alpha", Topics: []string{"go"}},
				{Owner: testDefaultOwner, Name: "beta", Topics: []string{"rust"}},
			},
		},
	})
	require.NoError(t, err)
	require.Empty(t, baseDir)
	require.Len(t, targets, 1)
	require.Equal(t, testDefaultOwner+"/alpha", targets[0].Slug)
}

func TestResolveCloneTargetsImplicitAllForLanguageFilters(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:      testDefaultOwner,
		Method:     methodSSH,
		VCS:        vcsGit,
		Languages:  []string{"Go"},
		Visibility: "all",
	}

	targets, baseDir, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{
		repos: map[string][]repoInfo{
			testDefaultOwner: {
				{Owner: testDefaultOwner, Name: "alpha", Language: "Go"},
				{Owner: testDefaultOwner, Name: "beta", Language: "Rust"},
			},
		},
	})
	require.NoError(t, err)
	require.Empty(t, baseDir)
	require.Len(t, targets, 1)
	require.Equal(t, testDefaultOwner+"/alpha", targets[0].Slug)
}

func TestResolveCloneTargetsLanguageFiltersOR(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:      testDefaultOwner,
		Repos:      []string{"all"},
		Method:     methodSSH,
		VCS:        vcsGit,
		Languages:  []string{"Go", "CLI"},
		Visibility: "all",
	}

	targets, _, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{
		repos: map[string][]repoInfo{
			testDefaultOwner: {
				{Owner: testDefaultOwner, Name: "alpha", Language: "Go"},
				{Owner: testDefaultOwner, Name: "beta", Language: "CLI"},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, targets, 2)
}

func TestResolveCloneTargetsDetectsDestinationClash(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:  testDefaultOwner,
		Repos:  []string{"foo/repo", "bar/repo"},
		VCS:    vcsGit,
		Method: methodSSH,
	}

	_, _, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{})
	require.Error(t, err)
}

func TestResolveCloneTargetsTopicFiltersAND(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:  testDefaultOwner,
		Repos:  []string{"all"},
		Method: methodSSH,
		VCS:    vcsGit,
		Topics: []string{"go", "cli"},
		TopicFilters: [][]string{
			{"go"},
			{"cli"},
		},
		Visibility: "all",
	}

	targets, _, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{
		repos: map[string][]repoInfo{
			testDefaultOwner: {
				{Owner: testDefaultOwner, Name: "alpha", Topics: []string{"go", "cli"}},
				{Owner: testDefaultOwner, Name: "beta", Topics: []string{"go"}},
				{Owner: testDefaultOwner, Name: "gamma", Topics: []string{"cli"}},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, testDefaultOwner+"/alpha", targets[0].Slug)
}

func TestResolveCloneTargetsTopicFiltersOR(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:  testDefaultOwner,
		Repos:  []string{"all"},
		Method: methodSSH,
		VCS:    vcsGit,
		Topics: []string{"go/rust"},
		TopicFilters: [][]string{
			{"go", "rust"},
		},
		Visibility: "all",
	}

	targets, _, err := resolveCloneTargets(context.Background(), cli, fakeRepoLister{
		repos: map[string][]repoInfo{
			testDefaultOwner: {
				{Owner: testDefaultOwner, Name: "alpha", Topics: []string{"go"}},
				{Owner: testDefaultOwner, Name: "beta", Topics: []string{"rust"}},
				{Owner: testDefaultOwner, Name: "gamma", Topics: []string{"python"}},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, targets, 2)
}

func TestFormatTopicFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		filters [][]string
		want    string
	}{
		{
			name:    "and",
			filters: [][]string{{"backend"}, {"cli"}},
			want:    "backend AND cli",
		},
		{
			name:    "or",
			filters: [][]string{{"backend", "cli"}},
			want:    "backend OR cli",
		},
		{
			name:    "mixed",
			filters: [][]string{{"backend", "platform"}, {"api"}},
			want:    "(backend OR platform) AND api",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, formatTopicFilters(test.filters))
		})
	}
}

func TestPluralize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		singular string
		filters  [][]string
		want     string
	}{
		{
			name:     "single value",
			singular: "language",
			filters:  [][]string{{"go"}},
			want:     "language",
		},
		{
			name:     "multiple in one group",
			singular: "language",
			filters:  [][]string{{"go", "python"}},
			want:     "languages",
		},
		{
			name:     "multiple groups",
			singular: "topic",
			filters:  [][]string{{"foo"}, {"bar"}},
			want:     "topics",
		},
		{
			name:     "empty",
			singular: "topic",
			want:     "topic",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, pluralize(test.singular, test.filters))
		})
	}
}
