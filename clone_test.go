package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCloner(t *testing.T) {
	t.Parallel()

	cloner := NewCloner(CloneTarget{
		Slug:         "owner/repo",
		Source:       "https://github.com/owner/repo.git",
		Dest:         "repo",
		VCS:          vcsGit,
		Depth:        1,
		SingleBranch: true,
	})
	require.NotNil(t, cloner)
	require.Equal(t, "https://github.com/owner/repo.git", cloner.Source)
	require.Equal(t, "owner/repo", cloner.Slug)
	require.Equal(t, "repo", cloner.Dest)
	require.True(t, cloner.SingleBranch)
}

func TestClonerGitCloneArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cloner *Cloner
		want   []string
	}{
		{
			name: "default",
			cloner: NewCloner(CloneTarget{
				Slug:   "owner/repo",
				Source: "https://github.com/owner/repo.git",
				Dest:   "repo",
				VCS:    vcsGit,
			}),
			want: []string{
				"clone",
				"--progress",
				"https://github.com/owner/repo.git",
				"repo",
			},
		},
		{
			name: "quick",
			cloner: NewCloner(CloneTarget{
				Slug:         "owner/repo",
				Source:       "https://github.com/owner/repo.git",
				Dest:         "repo",
				VCS:          vcsGit,
				Depth:        1,
				SingleBranch: true,
			}),
			want: []string{
				"clone",
				"--progress",
				"--single-branch",
				"--depth=1",
				"https://github.com/owner/repo.git",
				"repo",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, test.cloner.gitCloneArgs(true))
		})
	}
}

func TestClonerJJInitArgs(t *testing.T) {
	t.Parallel()

	cloner := NewCloner(CloneTarget{
		Slug:   "owner/repo",
		Source: "git@github.com:owner/repo.git",
		Dest:   "repo",
		VCS:    vcsJJ,
		Depth:  1,
		Branch: "main",
	})
	want := []string{
		"git",
		"init",
		"--color=never",
		"--colocate",
		".",
	}
	require.Equal(t, want, cloner.jjInitArgs())
}

func TestCloneCallbackMonotonic(t *testing.T) {
	t.Parallel()

	// Simulate the high-water mark logic from cloneCallback.Progress
	// without requiring a real clog.Update.
	var lastProgress int
	applyProgress := func(p *gitProgress) int {
		current := max((cloneProgress{Git: *p}).DisplayCurrent(), lastProgress)
		lastProgress = current
		return current
	}

	// Counting reaches 80%.
	p := &gitProgress{Counted: phaseProgress{Current: 80, Total: 100}}
	first := applyProgress(p)

	// Git revises the total upward - naive DisplayCurrent would drop.
	p.Counted = phaseProgress{Current: 81, Total: 10000}
	second := applyProgress(p)

	require.GreaterOrEqual(t, second, first)
	require.Equal(t, first, second)
}

func TestShowOverallProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		repoCount int
		want      bool
	}{
		{
			name:      "zero repos",
			repoCount: 0,
			want:      false,
		},
		{
			name:      "one repo",
			repoCount: 1,
			want:      false,
		},
		{
			name:      "four repos",
			repoCount: 4,
			want:      false,
		},
		{
			name:      "five repos",
			repoCount: 5,
			want:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, showOverallProgress(test.repoCount))
		})
	}
}

func TestClonerDryRunCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cloner *Cloner
		want   string
	}{
		{
			name: "git",
			cloner: NewCloner(CloneTarget{
				BinGit: "git",
				Slug:   "owner/repo",
				Source: "git@github.com:owner/repo.git",
				Dest:   "repo",
				VCS:    vcsGit,
			}),
			want: "git clone git@github.com:owner/repo.git repo",
		},
		{
			name: "jj two-step",
			cloner: NewCloner(CloneTarget{
				BinGit: "git",
				BinJJ:  "jj",
				Slug:   "owner/repo",
				Source: "git@github.com:owner/repo.git",
				Dest:   "repo",
				VCS:    vcsJJ,
			}),
			want: "git clone git@github.com:owner/repo.git repo\n" +
				"jj git init --color=never --colocate repo",
		},
		{
			name: "git with PR",
			cloner: &Cloner{
				BinGit:      "git",
				Slug:        "owner/repo",
				Source:      "git@github.com:owner/repo.git",
				Dest:        "repo",
				VCS:         vcsGit,
				PullRequest: "21",
				PRHeadRef:   "feature-branch",
			},
			want: "git clone git@github.com:owner/repo.git repo\n" +
				"git -C repo fetch origin refs/pull/21/head:feature-branch --no-tags\n" +
				"git -C repo checkout feature-branch",
		},
		{
			name: "jj with PR",
			cloner: &Cloner{
				BinGit:      "git",
				BinJJ:       "jj",
				Slug:        "owner/repo",
				Source:      "git@github.com:owner/repo.git",
				Dest:        "repo",
				VCS:         vcsJJ,
				PullRequest: "21",
				PRHeadRef:   "feature-branch",
			},
			want: "git clone git@github.com:owner/repo.git repo\n" +
				"jj git init --color=never --colocate repo\n" +
				"git -C repo fetch origin refs/pull/21/head:feature-branch --no-tags\n" +
				"jj -R repo git import\n" +
				"jj -R repo new feature-branch",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, test.cloner.DryRunCommand())
		})
	}
}
