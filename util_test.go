package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatCommandDryContractsHome(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	args := []string{"-C", filepath.Join(home, "code/repo"), "fetch"}

	require.Equal(
		t,
		"/opt/homebrew/bin/git -C ~/code/repo fetch",
		formatCommand("/opt/homebrew/bin/git", args, true),
	)
	require.Equal(
		t,
		"/opt/homebrew/bin/git -C "+filepath.Join(home, "code/repo")+" fetch",
		formatCommand("/opt/homebrew/bin/git", args, false),
	)
}

func TestCompactLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "single", input: "hello", want: "hello"},
		{name: "trims whitespace", input: "  a  \n  b  ", want: "a | b"},
		{name: "drops blank lines", input: "a\n\n\nb", want: "a | b"},
		{name: "dedupes", input: "a\nb\na\nb\nc", want: "a | b | c"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, compactLines(test.input))
		})
	}
}

func TestFormatCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		bin  string
		args []string
		want string
	}{
		{name: "no args", bin: "git", want: "git"},
		{name: "simple args", bin: "git", args: []string{"status"}, want: "git status"},
		{
			name: "quotes spaces",
			bin:  "git",
			args: []string{"-C", "/tmp/my repo", "fetch"},
			want: "git -C '/tmp/my repo' fetch",
		},
		{
			name: "quotes single quotes",
			bin:  "echo",
			args: []string{"it's"},
			want: `echo 'it'"'"'s'`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, formatCommand(test.bin, test.args, false))
		})
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "''"},
		{name: "plain", input: "abc", want: "abc"},
		{name: "space", input: "a b", want: "'a b'"},
		{name: "dollar", input: "$HOME", want: `'$HOME'`},
		{name: "double quote", input: `a"b`, want: `'a"b'`},
		{name: "single quote", input: "it's", want: `'it'"'"'s'`},
		{name: "tab", input: "a\tb", want: "'a\tb'"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, shellQuote(test.input))
		})
	}
}

func TestPathExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	got, err := pathExists(dir)
	require.NoError(t, err)
	require.True(t, got)

	got, err = pathExists(file)
	require.NoError(t, err)
	require.True(t, got)

	got, err = pathExists(filepath.Join(dir, "missing"))
	require.NoError(t, err)
	require.False(t, got)
}

func TestRunCommandInDir(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, runCommandInDir(context.Background(), "", "true", nil))
	})

	t.Run("failure surfaces stderr", func(t *testing.T) {
		t.Parallel()

		err := runCommandInDir(
			context.Background(),
			"",
			"sh",
			[]string{"-c", "echo boom >&2; exit 1"},
		)
		require.EqualError(t, err, "boom")
	})

	t.Run("runs in dir", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o600))
		// `ls marker` succeeds only if cwd is dir.
		require.NoError(t, runCommandInDir(context.Background(), dir, "ls", []string{"marker"}))
	})
}

func TestDetectVCS(t *testing.T) {
	t.Parallel()

	t.Run("jj wins over git for colocated repos", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
		require.NoError(t, os.Mkdir(filepath.Join(dir, ".jj"), 0o755))
		require.Equal(t, vcsJJ, detectVCS(dir, vcsGit))
	})

	t.Run("plain git", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
		require.Equal(t, vcsGit, detectVCS(dir, vcsJJ))
	})

	t.Run("plain jj", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, ".jj"), 0o755))
		require.Equal(t, vcsJJ, detectVCS(dir, vcsGit))
	})

	t.Run("neither marker falls back", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, vcsGit, detectVCS(t.TempDir(), vcsGit))
		require.Equal(t, vcsJJ, detectVCS(t.TempDir(), vcsJJ))
	})
}
