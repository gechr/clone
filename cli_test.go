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

func TestParseTopicFiltersCommaMeansAND(t *testing.T) {
	t.Parallel()

	got, err := parseTopicFilters([]string{"backend,cli"})
	require.NoError(t, err)
	require.Equal(t, [][]string{{"backend"}, {"cli"}}, got)
}

func TestParseTopicFiltersSlashMeansOR(t *testing.T) {
	t.Parallel()

	got, err := parseTopicFilters([]string{"backend/cli"})
	require.NoError(t, err)
	require.Equal(t, [][]string{{"backend", "cli"}}, got)
}

func TestParseTopicFiltersRepeatedFlagsMeanAND(t *testing.T) {
	t.Parallel()

	got, err := parseTopicFilters([]string{"backend", "cli"})
	require.NoError(t, err)
	require.Equal(t, [][]string{{"backend"}, {"cli"}}, got)
}

func TestParseTopicFiltersRejectsEmptyString(t *testing.T) {
	t.Parallel()

	_, err := parseTopicFilters([]string{""})
	require.EqualError(t, err, `invalid topic filter ""`)
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
