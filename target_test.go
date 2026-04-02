package main

import (
	"context"
	"fmt"
	"testing"
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
		if len(opts.Topics) > 0 && !matchesAnyFold(opts.Topics, repo.Topics...) {
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
			if err != nil {
				t.Fatalf("parseRepoRequest() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("parseRepoRequest() = %#v, want %#v", got, test.want)
			}
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
			if err != nil {
				t.Fatalf("parseRepoRequest() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("parseRepoRequest() = %#v, want %#v", got, test.want)
			}
		})
	}
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
	if err != nil {
		t.Fatalf("resolveCloneTargets() error = %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}
	// Single same-repo open PR: should use --branch optimization
	if got, want := targets[0].Branch, "feature-branch"; got != want {
		t.Fatalf("Branch = %q, want %q", got, want)
	}
	if targets[0].PullRequest != "" {
		t.Fatalf("PullRequest = %q, want empty (resolved to --branch)", targets[0].PullRequest)
	}
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
	if err == nil {
		t.Fatal("resolveCloneTargets() error = nil, want conflict error")
	}
}

func TestResolveCloneTargetsAllAndFilters(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Owner:      testDefaultOwner,
		Repos:      []string{"all"},
		Method:     methodSSH,
		VCS:        vcsGit,
		Topics:     []string{"go"},
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
	if err != nil {
		t.Fatalf("resolveCloneTargets() error = %v", err)
	}
	if baseDir != "" {
		t.Fatalf("baseDir = %q, want empty", baseDir)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}
	if got, want := targets[0].Slug, testDefaultOwner+"/alpha"; got != want {
		t.Fatalf("Slug = %q, want %q", got, want)
	}
	if got, want := targets[0].Source, "git@github.com:"+testDefaultOwner+"/alpha.git"; got != want {
		t.Fatalf("Source = %q, want %q", got, want)
	}
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
	if err == nil {
		t.Fatal("resolveCloneTargets() error = nil, want clash error")
	}
}
