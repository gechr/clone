package main

import (
	"io"
	"os"
	"testing"

	"github.com/gechr/clog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildParserQuick(t *testing.T) {
	t.Parallel()

	var cli CLI
	parser := buildParser(&cli)
	_, err := parser.Parse([]string{"-Q", "owner/repo"})
	require.NoError(t, err)

	cli.Normalize()
	assert.Equal(t, 1, cli.Depth)
	assert.True(t, cli.Quick)
	assert.Equal(t, []string{"owner/repo"}, cli.Repos)
}

func TestBuildParserAttachedShortFlags(t *testing.T) {
	t.Parallel()

	var cli CLI
	parser := buildParser(&cli)
	_, err := parser.Parse([]string{"-D10", "-P5", "owner/repo"})
	require.NoError(t, err)

	assert.Equal(t, 10, cli.Depth)
	assert.Equal(t, 5, cli.Parallelism)
}

func TestBuildParserFlagAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		check func(t *testing.T, cli *CLI)
	}{
		{
			name: "bookmark",
			args: []string{"--bookmark=main", "repo"},
			check: func(t *testing.T, cli *CLI) {
				t.Helper()
				assert.Equal(t, "main", cli.Branch)
			},
		},
		{
			name: "org",
			args: []string{"--org=owner", "repo"},
			check: func(t *testing.T, cli *CLI) {
				t.Helper()
				assert.Equal(t, "owner", cli.Owner)
			},
		},
		{
			name: "organization",
			args: []string{"--organization", "owner", "repo"},
			check: func(t *testing.T, cli *CLI) {
				t.Helper()
				assert.Equal(t, "owner", cli.Owner)
			},
		},
		{
			name: "archive",
			args: []string{"--archive", "all"},
			check: func(t *testing.T, cli *CLI) {
				t.Helper()
				assert.True(t, cli.Archived)
			},
		},
		{
			name: "archives",
			args: []string{"--archives", "all"},
			check: func(t *testing.T, cli *CLI) {
				t.Helper()
				assert.True(t, cli.Archived)
			},
		},
		{
			name: "fork",
			args: []string{"--fork", "all"},
			check: func(t *testing.T, cli *CLI) {
				t.Helper()
				assert.True(t, cli.Forked)
			},
		},
		{
			name: "forks",
			args: []string{"--forks", "all"},
			check: func(t *testing.T, cli *CLI) {
				t.Helper()
				assert.True(t, cli.Forked)
			},
		},
		{
			name: "dry-run",
			args: []string{"--dry-run", "repo"},
			check: func(t *testing.T, cli *CLI) {
				t.Helper()
				assert.True(t, cli.Dry)
			},
		},
		{
			name: "dir",
			args: []string{"--dir=/tmp", "repo"},
			check: func(t *testing.T, cli *CLI) {
				t.Helper()
				assert.Equal(t, "/tmp", cli.Directory)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var cli CLI
			parser := buildParser(&cli)
			_, err := parser.Parse(test.args)
			require.NoError(t, err)
			test.check(t, &cli)
		})
	}
}

func TestBuildParserMethodHTTP(t *testing.T) {
	t.Parallel()

	var cli CLI
	parser := buildParser(&cli)
	_, err := parser.Parse([]string{"--method=http", "repo"})
	require.NoError(t, err)

	cli.Normalize()
	assert.Equal(t, methodHTTPS, cli.Method)
}

func TestConfigureClogWritesErrorsToStderr(t *testing.T) {
	originalStdout := os.Stdout
	originalStderr := os.Stderr
	originalLogger := clog.Default

	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)
	stderrR, stderrW, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = stdoutW
	os.Stderr = stderrW
	clog.Default = clog.New(clog.Stdout(clog.ColorNever))

	t.Cleanup(func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		clog.Default = originalLogger
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
	})

	configureClog()
	clog.SetColorMode(clog.ColorNever)
	clog.Error().Msg("boom")

	require.NoError(t, stdoutW.Close())
	require.NoError(t, stderrW.Close())

	stdout, err := io.ReadAll(stdoutR)
	require.NoError(t, err)
	stderr, err := io.ReadAll(stderrR)
	require.NoError(t, err)

	assert.Equal(t, "", string(stdout))
	assert.Contains(t, string(stderr), "✘ boom")
}
