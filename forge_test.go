package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseForgeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  repoRequest
	}{
		// GitHub
		{
			name:  "github https",
			input: "https://github.com/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			name:  "github https trailing slash",
			input: "https://github.com/owner/repo/",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			name:  "github https .git suffix",
			input: "https://github.com/owner/repo.git",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			name:  "github https commits page",
			input: "https://github.com/jurplel/InstantSpaceSwitcher/commits/main/",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "jurplel",
				Name:          "InstantSpaceSwitcher",
				Source:        "https://github.com/jurplel/InstantSpaceSwitcher.git",
			},
		},
		{
			name:  "github https tree path",
			input: "https://github.com/owner/repo/tree/develop/src/foo.go",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			name:  "github https blob path",
			input: "https://github.com/owner/repo/blob/main/README.md",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			name:  "github https actions",
			input: "https://github.com/owner/repo/actions/runs/12345",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			name:  "github https pull request",
			input: "https://github.com/owner/repo/pull/42",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
				PullRequest:   "42",
			},
		},
		{
			name:  "github https www prefix",
			input: "https://www.github.com/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			name:  "github http",
			input: "http://github.com/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			name:  "github ssh",
			input: "git@github.com:owner/repo.git",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "git@github.com:owner/repo.git",
			},
		},
		{
			name:  "github bare host",
			input: "github.com/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
			},
		},
		{
			name:  "github bare host with PR fragment",
			input: "github.com/owner/repo#21",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "github.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://github.com/owner/repo.git",
				PullRequest:   "21",
			},
		},

		// GitLab
		{
			name:  "gitlab https",
			input: "https://gitlab.com/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "gitlab.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://gitlab.com/owner/repo.git",
			},
		},
		{
			name:  "gitlab https tree via dash separator",
			input: "https://gitlab.com/owner/repo/-/tree/main",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "gitlab.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://gitlab.com/owner/repo.git",
			},
		},
		{
			name:  "gitlab nested group with dash separator",
			input: "https://gitlab.com/group/subgroup/repo/-/tree/main",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "gitlab.com",
				Owner:         "group/subgroup",
				Name:          "repo",
				Source:        "https://gitlab.com/group/subgroup/repo.git",
			},
		},
		{
			name:  "gitlab ssh",
			input: "git@gitlab.com:owner/repo.git",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "gitlab.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "git@gitlab.com:owner/repo.git",
			},
		},
		{
			name:  "gitlab bare host",
			input: "gitlab.com/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "gitlab.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://gitlab.com/owner/repo.git",
			},
		},

		// Codeberg / Forgejo
		{
			name:  "codeberg https",
			input: "https://codeberg.org/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "codeberg.org",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://codeberg.org/owner/repo.git",
			},
		},
		{
			name:  "codeberg https src path",
			input: "https://codeberg.org/owner/repo/src/branch/main/file.go",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "codeberg.org",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://codeberg.org/owner/repo.git",
			},
		},
		{
			name:  "codeberg ssh",
			input: "git@codeberg.org:owner/repo.git",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "codeberg.org",
				Owner:         "owner",
				Name:          "repo",
				Source:        "git@codeberg.org:owner/repo.git",
			},
		},

		// Bitbucket
		{
			name:  "bitbucket https",
			input: "https://bitbucket.org/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "bitbucket.org",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://bitbucket.org/owner/repo.git",
			},
		},
		{
			name:  "bitbucket https src path",
			input: "https://bitbucket.org/owner/repo/src/main/README.md",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "bitbucket.org",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://bitbucket.org/owner/repo.git",
			},
		},
		{
			name:  "bitbucket ssh",
			input: "git@bitbucket.org:owner/repo.git",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "bitbucket.org",
				Owner:         "owner",
				Name:          "repo",
				Source:        "git@bitbucket.org:owner/repo.git",
			},
		},

		// Sourcehut
		{
			name:  "sourcehut https",
			input: "https://git.sr.ht/~sircmpwn/scdoc",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "git.sr.ht",
				Owner:         "~sircmpwn",
				Name:          "scdoc",
				Source:        "https://git.sr.ht/~sircmpwn/scdoc",
			},
		},
		{
			name:  "sourcehut https log path",
			input: "https://git.sr.ht/~sircmpwn/scdoc/log/main",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "git.sr.ht",
				Owner:         "~sircmpwn",
				Name:          "scdoc",
				Source:        "https://git.sr.ht/~sircmpwn/scdoc",
			},
		},
		{
			name:  "sourcehut ssh",
			input: "git@git.sr.ht:~sircmpwn/scdoc",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "git.sr.ht",
				Owner:         "~sircmpwn",
				Name:          "scdoc",
				Source:        "git@git.sr.ht:~sircmpwn/scdoc",
			},
		},

		// Azure DevOps
		{
			name:  "azure devops https",
			input: "https://dev.azure.com/org/project/_git/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "dev.azure.com",
				Owner:         "org",
				Name:          "repo",
				Source:        "https://dev.azure.com/org/project/_git/repo",
			},
		},

		// Generic / unknown forges (self-hosted)
		{
			name:  "self-hosted gitea https",
			input: "https://git.example.com/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "git.example.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://git.example.com/owner/repo.git",
			},
		},
		{
			name:  "self-hosted with ui path",
			input: "https://git.example.com/owner/repo/commits/main",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "git.example.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://git.example.com/owner/repo.git",
			},
		},
		{
			name:  "unknown forge ssh",
			input: "git@git.example.com:owner/repo.git",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "git.example.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "git@git.example.com:owner/repo.git",
			},
		},
		{
			name:  "unknown forge bare host",
			input: "git.example.com/owner/repo",
			want: repoRequest{
				ExplicitOwner: true,
				Host:          "git.example.com",
				Owner:         "owner",
				Name:          "repo",
				Source:        "https://git.example.com/owner/repo.git",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseForgeURL(test.input)
			require.True(t, ok, "parseForgeURL(%q) returned false", test.input)
			require.Equal(t, test.want, got)
		})
	}
}

func TestParseForgeURLRejectsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "plain word", input: "repo"},
		{name: "shorthand", input: "owner/repo"},
		{name: "empty", input: ""},
		{name: "host only", input: "https://github.com/"},
		{name: "host owner only", input: "https://github.com/owner"},
		{name: "azure no _git", input: "https://dev.azure.com/org/project/repo"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, ok := parseForgeURL(test.input)
			require.False(t, ok, "parseForgeURL(%q) should return false", test.input)
		})
	}
}
