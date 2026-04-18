package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCLINormalizeKeepsExplicitOwner(t *testing.T) {
	t.Parallel()

	cli := &CLI{Owner: "cli"}
	cli.Normalize()

	require.Equal(t, "cli", cli.Owner)
}

func TestCLINormalizeMethodHTTPToHTTPS(t *testing.T) {
	t.Parallel()

	cli := &CLI{Method: "http"}
	cli.Normalize()

	require.Equal(t, methodHTTPS, cli.Method)
}

func TestCLINormalizeMethodHTTPSUnchanged(t *testing.T) {
	t.Parallel()

	cli := &CLI{Method: methodHTTPS}
	cli.Normalize()

	require.Equal(t, methodHTTPS, cli.Method)
}

func TestCLINormalizeDeduplicatesLanguages(t *testing.T) {
	t.Parallel()

	cli := &CLI{Languages: []string{"go", "go", "Go", "rust"}}
	cli.Normalize()

	require.Equal(t, []string{"go", "rust"}, cli.Languages)
}

func TestCLIValidateDeduplicatesTopicFilters(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Repos:       []string{"all"},
		Topics:      []string{"go/cli", "CLI/go", "go", "Go"},
		Visibility:  keywordAll,
		Parallelism: defaultParallelism,
	}

	require.NoError(t, cli.Validate())
	require.Equal(t, [][]string{{"go", "cli"}, {"go"}}, cli.TopicFilters)
}

func TestParseFiltersCommaMeansAND(t *testing.T) {
	t.Parallel()

	got, err := parseFilters("topic", []string{"backend,cli"})
	require.NoError(t, err)
	require.Equal(t, [][]string{{"backend"}, {"cli"}}, got)
}

func TestParseFiltersSlashMeansOR(t *testing.T) {
	t.Parallel()

	got, err := parseFilters("topic", []string{"backend/cli"})
	require.NoError(t, err)
	require.Equal(t, [][]string{{"backend", "cli"}}, got)
}

func TestParseFiltersRepeatedFlagsMeanAND(t *testing.T) {
	t.Parallel()

	got, err := parseFilters("topic", []string{"backend", "cli"})
	require.NoError(t, err)
	require.Equal(t, [][]string{{"backend"}, {"cli"}}, got)
}

func TestParseFiltersRejectsEmptyString(t *testing.T) {
	t.Parallel()

	_, err := parseFilters("topic", []string{""})
	require.EqualError(t, err, `invalid topic filter ""`)

	_, err = parseFilters("language", []string{""})
	require.EqualError(t, err, `invalid language filter ""`)
}

func TestCLIValidateParsesTopicFilters(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Repos:       []string{"all"},
		Topics:      []string{"backend/cli", "api"},
		Visibility:  keywordAll,
		Parallelism: defaultParallelism,
	}

	require.NoError(t, cli.Validate())
	require.Equal(t, "(backend OR cli) AND api", formatTopicFilters(cli.TopicFilters))
}

func TestCLIValidateParsesLanguageFilters(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Repos:       []string{"all"},
		Languages:   []string{"a/b"},
		Visibility:  keywordAll,
		Parallelism: defaultParallelism,
	}

	require.NoError(t, cli.Validate())
	require.Equal(t, []string{"a", "b"}, cli.Languages)
	require.Equal(t, [][]string{{"a", "b"}}, cli.LanguageFilters)
}

func TestCLIValidateForgeDefaultsToGitHub(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Repos:       []string{"owner/repo"},
		Visibility:  keywordAll,
		Parallelism: defaultParallelism,
	}

	require.NoError(t, cli.Validate())
	require.Equal(t, hostGitHub, cli.forge.Host)
}

func TestCLIValidateForgeAcceptsName(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Repos:       []string{"owner/repo"},
		Forge:       "gitlab",
		Visibility:  keywordAll,
		Parallelism: defaultParallelism,
	}

	require.NoError(t, cli.Validate())
	require.Equal(t, hostGitLab, cli.forge.Host)
}

func TestCLIValidateForgeAcceptsCustomHost(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Repos:       []string{"owner/repo"},
		Forge:       "git.example.com",
		Visibility:  keywordAll,
		Parallelism: defaultParallelism,
	}

	require.NoError(t, cli.Validate())
	require.Equal(t, "git.example.com", cli.forge.Host)
}

func TestCLIValidateForgeRejectsGitHubOnlyFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cli  CLI
		msg  string
	}{
		{
			name: "archived",
			cli:  CLI{Archived: true},
			msg:  "--archived is only currently supported for GitHub hosts",
		},
		{
			name: "language",
			cli:  CLI{Languages: []string{"go"}},
			msg:  "--language is only currently supported for GitHub hosts",
		},
		{
			name: "topic",
			cli:  CLI{Topics: []string{"cli"}},
			msg:  "--topic is only currently supported for GitHub hosts",
		},
		{
			name: "multiple joined with slash",
			cli:  CLI{Archived: true, Forked: true},
			msg:  "--archived/--forked is only currently supported for GitHub hosts",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := test.cli
			c.Repos = []string{"owner/repo"}
			c.Forge = "gitlab"
			c.Visibility = keywordAll
			c.Parallelism = defaultParallelism

			err := c.Validate()
			require.EqualError(t, err, test.msg)
		})
	}
}

func TestCLIValidateFetchAlone(t *testing.T) {
	t.Parallel()

	cli := &CLI{
		Repos:       []string{"repo"},
		Fetch:       true,
		Visibility:  keywordAll,
		Parallelism: defaultParallelism,
	}

	require.NoError(t, cli.Validate())
}
