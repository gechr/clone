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
		"--config",
		`signing.behavior="drop"`,
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

func TestClonerLinkUsesPRIdentity(t *testing.T) {
	t.Parallel()

	cloner := NewCloner(CloneTarget{
		Label:   "owner/repo",
		Slug:    "owner/repo",
		RepoURL: "https://github.com/owner/repo",
		PRLabel: "21",
	})

	require.Equal(t, "repository", cloner.LinkKey())
	require.Equal(t, "owner/repo", cloner.LinkText())
	require.Equal(t, "https://github.com/owner/repo", cloner.LinkURL())
	require.Equal(t, "pr", cloner.RefKey())
	require.Equal(t, "21", cloner.RefText())
	require.Equal(t, "https://github.com/owner/repo/pull/21", cloner.RefURL())
}

func TestClonerLinkUsesRepositoryIdentity(t *testing.T) {
	t.Parallel()

	cloner := NewCloner(CloneTarget{
		Label:   "repo",
		Slug:    "owner/repo",
		RepoURL: "https://github.com/owner/repo",
	})

	require.Equal(t, "repository", cloner.LinkKey())
	require.Equal(t, "repo", cloner.LinkText())
	require.Equal(t, "https://github.com/owner/repo", cloner.LinkURL())
	require.Empty(t, cloner.RefKey())
	require.Empty(t, cloner.RefText())
	require.Empty(t, cloner.RefURL())
}

func TestClonerRefLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cloner  *Cloner
		wantKey string
		wantURL string
	}{
		{
			name: "commit",
			cloner: NewCloner(CloneTarget{
				Commit:  "83c74cc3e85aeaa4b63de7dc529909791de67206",
				RepoURL: "https://github.com/owner/repo",
			}),
			wantKey: "commit",
			wantURL: "https://github.com/owner/repo/commit/83c74cc3e85aeaa4b63de7dc529909791de67206",
		},
		{
			name: "tag",
			cloner: NewCloner(CloneTarget{
				Branch:  "release/v1.2.3",
				Tag:     "release/v1.2.3",
				RepoURL: "https://github.com/owner/repo",
			}),
			wantKey: "tag",
			wantURL: "https://github.com/owner/repo/releases/tag/release%2Fv1.2.3",
		},
		{
			name: "branch",
			cloner: NewCloner(CloneTarget{
				Branch:  "feature/logging",
				RepoURL: "https://github.com/owner/repo",
			}),
			wantKey: "branch",
			wantURL: "https://github.com/owner/repo/tree/feature%2Flogging",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.wantKey, test.cloner.RefKey())
			wantText := test.cloner.Branch
			if test.cloner.Commit != "" {
				wantText = "83c74cc3"
			}
			require.Equal(t, wantText, test.cloner.RefText())
			require.Equal(t, test.wantURL, test.cloner.RefURL())
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
			want: "git clone git@github.com:owner/repo.git repo" + dryRunSep() + "" +
				`jj --config 'signing.behavior="drop"' git init --color=never --colocate repo`,
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
			want: "git clone git@github.com:owner/repo.git repo" + dryRunSep() + "" +
				"git -C repo fetch origin refs/pull/21/head:feature-branch --no-tags" + dryRunSep() + "" +
				"git -C repo checkout feature-branch",
		},
		{
			name: "git with commit",
			cloner: &Cloner{
				BinGit: "git",
				Commit: "83c74cc3e85aeaa4b63de7dc529909791de67206",
				Source: "git@github.com:owner/repo.git",
				Dest:   "repo",
				VCS:    vcsGit,
			},
			want: "git clone git@github.com:owner/repo.git repo" + dryRunSep() + "" +
				"git -C repo fetch origin 83c74cc3e85aeaa4b63de7dc529909791de67206 --no-tags" + dryRunSep() + "" +
				"git -C repo checkout 83c74cc3e85aeaa4b63de7dc529909791de67206",
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
			want: "git clone git@github.com:owner/repo.git repo" + dryRunSep() + "" +
				`jj --config 'signing.behavior="drop"' git init --color=never --colocate repo` + dryRunSep() + "" +
				"git -C repo fetch origin refs/pull/21/head:feature-branch --no-tags" + dryRunSep() + "" +
				`jj --config 'signing.behavior="drop"' -R repo git import` + dryRunSep() + "" +
				`jj --config 'signing.behavior="drop"' -R repo new feature-branch`,
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
			want: "git -C repo fetch" + dryRunSep() +
				`jj --config 'signing.behavior="drop"' -R repo git import --quiet`,
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

	cloners, fetchers, err := prepareCloners(targets, prepareCloneOpts{Fetch: true})
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

	cloners, fetchers, err := prepareCloners(targets, prepareCloneOpts{})
	require.NoError(t, err)
	require.Len(t, cloners, 1)
	require.Equal(t, "owner/new-repo", cloners[0].Slug)
	require.Empty(t, fetchers)
}
