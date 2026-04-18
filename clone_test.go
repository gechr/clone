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
			want: "git clone git@github.com:owner/repo.git repo && " +
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
			want: "git clone git@github.com:owner/repo.git repo && " +
				"git -C repo fetch origin refs/pull/21/head:feature-branch --no-tags && " +
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
			want: "git clone git@github.com:owner/repo.git repo && " +
				"jj git init --color=never --colocate repo && " +
				"git -C repo fetch origin refs/pull/21/head:feature-branch --no-tags && " +
				"jj -R repo git import && " +
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

func TestNewFetcher(t *testing.T) {
	t.Parallel()

	fetcher := NewFetcher(CloneTarget{
		BinGit:  "git",
		BinJJ:   "jj",
		Slug:    "owner/repo",
		Dest:    "repo",
		Label:   "owner/repo",
		RepoURL: "https://github.com/owner/repo",
		VCS:     vcsGit,
	}, false, false)
	require.NotNil(t, fetcher)
	require.Equal(t, "git", fetcher.BinGit)
	require.Equal(t, "jj", fetcher.BinJJ)
	require.Equal(t, "owner/repo", fetcher.Slug)
	require.Equal(t, "repo", fetcher.Dest)
	require.Equal(t, vcsGit, fetcher.VCS)
}

func TestFetcherGitFetchArgs(t *testing.T) {
	t.Parallel()

	fetcher := &Fetcher{BinGit: "git", Dest: "repo", VCS: vcsGit}

	require.Equal(t, []string{"-C", "repo", "fetch", "--progress"}, fetcher.gitFetchArgs(true))
	require.Equal(t, []string{"-C", "repo", "fetch"}, fetcher.gitFetchArgs(false))
}

func TestFetcherGitPullArgs(t *testing.T) {
	t.Parallel()

	fetcher := &Fetcher{BinGit: "git", Dest: "repo", VCS: vcsGit, Pull: true}
	require.Equal(
		t,
		[]string{"-C", "repo", "pull", "--rebase", "--progress"},
		fetcher.gitFetchArgs(true),
	)
	require.Equal(t, []string{"-C", "repo", "pull", "--rebase"}, fetcher.gitFetchArgs(false))
}

func TestFetcherGitPullForceArgs(t *testing.T) {
	t.Parallel()

	fetcher := &Fetcher{BinGit: "git", Dest: "repo", VCS: vcsGit, Pull: true, Force: true}
	require.Equal(
		t,
		[]string{"-C", "repo", "pull", "--rebase", "--force"},
		fetcher.gitFetchArgs(false),
	)
}

func TestFetcherJJPullStillUsesFetch(t *testing.T) {
	t.Parallel()

	// For jj-colocated repos, --pull collapses to --fetch behavior.
	fetcher := &Fetcher{BinGit: "git", Dest: "repo", VCS: vcsJJ, Pull: true, Force: true}
	require.Equal(t, []string{"-C", "repo", "fetch"}, fetcher.gitFetchArgs(false))
}

func TestFetcherDryRunCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fetcher *Fetcher
		want    string
	}{
		{
			name:    "git",
			fetcher: &Fetcher{BinGit: "git", Dest: "repo", VCS: vcsGit},
			want:    "git -C repo fetch",
		},
		{
			name:    "jj",
			fetcher: &Fetcher{BinGit: "git", BinJJ: "jj", Dest: "repo", VCS: vcsJJ},
			want:    "git -C repo fetch && jj -R repo git import --quiet",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, test.fetcher.DryRunCommand())
		})
	}
}

func TestPrepareClonersWithFetch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	targets := []CloneTarget{
		{
			BinGit: "git",
			Slug:   "owner/existing",
			Label:  "owner/existing",
			Dest:   dir, // exists
			VCS:    vcsGit,
		},
		{
			BinGit: "git",
			Slug:   "owner/new-repo",
			Label:  "owner/new-repo",
			Dest:   dir + "/nonexistent",
			VCS:    vcsGit,
		},
	}

	cloners, fetchers, err := prepareCloners(targets, false, false, false, true, false)
	require.NoError(t, err)
	require.Len(t, cloners, 1)
	require.Equal(t, "owner/new-repo", cloners[0].Slug)
	require.Len(t, fetchers, 1)
	require.Equal(t, "owner/existing", fetchers[0].Slug)
}

func TestGroupFooterLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cloners    []*Cloner
		fetchers   []*Fetcher
		wantActive string
		wantDone   string
	}{
		{
			name:       "clones only",
			cloners:    []*Cloner{{}},
			wantActive: "Cloning",
			wantDone:   "Cloned",
		},
		{
			name:       "fetches only",
			fetchers:   []*Fetcher{{}},
			wantActive: "Fetching",
			wantDone:   "Fetched",
		},
		{
			name:       "mixed",
			cloners:    []*Cloner{{}},
			fetchers:   []*Fetcher{{}},
			wantActive: "Syncing",
			wantDone:   "Synced",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			active, done := groupFooterLabel(test.cloners, test.fetchers)
			require.Equal(t, test.wantActive, active)
			require.Equal(t, test.wantDone, done)
		})
	}
}

func TestPrepareClonersWithoutFetch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	targets := []CloneTarget{
		{
			BinGit: "git",
			Slug:   "owner/existing",
			Label:  "owner/existing",
			Dest:   dir, // exists
			VCS:    vcsGit,
		},
		{
			BinGit: "git",
			Slug:   "owner/new-repo",
			Label:  "owner/new-repo",
			Dest:   dir + "/nonexistent",
			VCS:    vcsGit,
		},
	}

	cloners, fetchers, err := prepareCloners(targets, false, false, false, false, false)
	require.NoError(t, err)
	require.Len(t, cloners, 1)
	require.Equal(t, "owner/new-repo", cloners[0].Slug)
	require.Empty(t, fetchers)
}
