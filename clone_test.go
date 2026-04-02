package main

import (
	"reflect"
	"testing"
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
	if cloner == nil {
		t.Fatal("NewCloner() = nil")
	}
	if got, want := cloner.Source, "https://github.com/owner/repo.git"; got != want {
		t.Fatalf("Source = %q, want %q", got, want)
	}
	if got, want := cloner.Slug, "owner/repo"; got != want {
		t.Fatalf("Slug = %q, want %q", got, want)
	}
	if got, want := cloner.Dest, "repo"; got != want {
		t.Fatalf("Dest = %q, want %q", got, want)
	}
	if !cloner.SingleBranch {
		t.Fatal("SingleBranch = false, want true")
	}
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

			if got := test.cloner.gitCloneArgs(true); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("gitCloneArgs() = %#v, want %#v", got, test.want)
			}
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
	if got := cloner.jjInitArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("jjInitArgs() = %#v, want %#v", got, want)
	}
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

	if second < first {
		t.Fatalf("progress went backwards: %d -> %d", first, second)
	}
	if second != first {
		t.Fatalf("expected high-water mark %d, got %d", first, second)
	}
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

			if got := showOverallProgress(test.repoCount); got != test.want {
				t.Fatalf("showOverallProgress(%d) = %v, want %v", test.repoCount, got, test.want)
			}
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

			if got := test.cloner.DryRunCommand(); got != test.want {
				t.Fatalf("DryRunCommand() = %q, want %q", got, test.want)
			}
		})
	}
}
