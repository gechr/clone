package main

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadProgressCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		current int
		total   int
		ok      bool
	}{
		{
			name:    "receiving",
			line:    "Receiving objects:  42% (42/100), 1.23 MiB | 1.23 MiB/s",
			current: 42,
			total:   100,
			ok:      true,
		},
		{
			name:    "resolving zero",
			line:    "Resolving deltas: 100% (0/0), done.",
			current: 0,
			total:   0,
			ok:      true,
		},
		{
			name: "missing counts",
			line: "remote: Enumerating objects: 42, done.",
			ok:   false,
		},
		{
			name: "invalid counts",
			line: "Receiving objects: 100% (420/100), done.",
			ok:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			current, total, ok := readProgressCounts(test.line)
			require.Equal(t, test.ok, ok)
			require.Equal(t, test.current, current)
			require.Equal(t, test.total, total)
		})
	}
}

func TestGitProgressApply(t *testing.T) {
	t.Parallel()

	progress := gitProgress{}

	require.True(t, progress.apply("remote: Counting objects: 100% (10/10), done."))
	require.True(t, progress.apply("Receiving objects:  50% (5/10), 1.23 MiB | 1.23 MiB/s"))
	require.True(t, progress.apply("Resolving deltas: 100% (3/3), done."))
	require.True(t, progress.apply("Updating files: 100% (20/20), done."))
	require.True(t, progress.apply("Filtering content: 100% (1/1), 233.51 MiB | 6.14 MiB/s, done."))
	require.False(t, progress.apply("remote: Enumerating objects: 42, done."))
	require.Equal(t, 39, progress.Current())
	require.Equal(t, 44, progress.Total())
}

func TestGitProgressOverall(t *testing.T) {
	t.Parallel()

	p := gitProgress{}
	require.InDelta(t, 0.0, p.Overall(), 1e-9)

	p.Objects = phaseProgress{Current: 50, Total: 100}
	require.InDelta(t, 0.5, p.Overall(), 1e-9)
}

func TestCloneProgressDisplayCurrent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    cloneProgress
		want int
	}{
		{
			name: "counting",
			p: cloneProgress{
				Git: gitProgress{
					Counted: phaseProgress{Current: 5, Total: 10},
				},
			},
			want: 50,
		},
		{
			name: "compressing",
			p: cloneProgress{
				Git: gitProgress{
					Counted:    phaseProgress{Current: 10, Total: 10},
					Compressed: phaseProgress{Current: 1, Total: 10},
				},
			},
			want: 110,
		},
		{
			name: "receiving",
			p: cloneProgress{
				Git: gitProgress{
					Counted:    phaseProgress{Current: 10, Total: 10},
					Compressed: phaseProgress{Current: 10, Total: 10},
					Objects:    phaseProgress{Current: 5, Total: 10},
				},
			},
			want: 450,
		},
		{
			name: "resolving",
			p: cloneProgress{
				Git: gitProgress{
					Counted:    phaseProgress{Current: 10, Total: 10},
					Compressed: phaseProgress{Current: 10, Total: 10},
					Objects:    phaseProgress{Current: 10, Total: 10},
					Deltas:     phaseProgress{Current: 5, Total: 10},
				},
			},
			want: 750,
		},
		{
			name: "updating",
			p: cloneProgress{
				Git: gitProgress{
					Counted:    phaseProgress{Current: 10, Total: 10},
					Compressed: phaseProgress{Current: 10, Total: 10},
					Objects:    phaseProgress{Current: 10, Total: 10},
					Deltas:     phaseProgress{Current: 10, Total: 10},
					Files:      phaseProgress{Current: 5, Total: 10},
				},
			},
			want: 850,
		},
		{
			name: "filtering",
			p: cloneProgress{
				Git: gitProgress{
					Counted:       phaseProgress{Current: 10, Total: 10},
					Compressed:    phaseProgress{Current: 10, Total: 10},
					Objects:       phaseProgress{Current: 10, Total: 10},
					Deltas:        phaseProgress{Current: 10, Total: 10},
					Files:         phaseProgress{Current: 10, Total: 10},
					FilterContent: phaseProgress{Current: 5, Total: 10},
				},
			},
			want: 950,
		},
		{
			name: "lfs checkout",
			p: cloneProgress{
				Git: gitProgress{
					Counted:    phaseProgress{Current: 10, Total: 10},
					Compressed: phaseProgress{Current: 10, Total: 10},
					Objects:    phaseProgress{Current: 10, Total: 10},
					Deltas:     phaseProgress{Current: 10, Total: 10},
					Files:      phaseProgress{Current: 10, Total: 10},
				},
				LFS: lfsProgress{
					Operation:    "checkout",
					CurrentFile:  5,
					TotalFiles:   10,
					CurrentBytes: 50,
					TotalBytes:   100,
					Name:         "asset.bin",
				},
			},
			want: 945,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, test.p.DisplayCurrent())
		})
	}
}

func TestCloneProgressDisplayTotal(t *testing.T) {
	t.Parallel()

	p := cloneProgress{}
	require.Equal(t, cloneDisplayTotal-filesWeight-checkoutWeight, p.DisplayTotal())

	p.Git.Files = phaseProgress{Current: 1, Total: 2}
	require.Equal(t, cloneDisplayTotal-checkoutWeight, p.DisplayTotal())

	p.LFS = lfsProgress{CurrentFile: 1, TotalFiles: 2}
	require.Equal(t, cloneDisplayTotal, p.DisplayTotal())
}

func TestCloneProgressDisplayStateCapsIncompleteTotal(t *testing.T) {
	t.Parallel()

	p := cloneProgress{
		Git: gitProgress{
			Deltas: phaseProgress{Current: 10, Total: 10},
		},
	}

	current, total := p.DisplayState(0)
	require.Equal(t, cloneDisplayTotal-filesWeight-checkoutWeight, total)
	require.Less(t, current, total)
	require.Equal(t, pendingProgressValue(total), current)
}

func TestCloneProgressDisplayStateParksNonLFSCheckoutAtPending(t *testing.T) {
	t.Parallel()

	p := cloneProgress{
		Git: gitProgress{
			Counted:    phaseProgress{Current: 10, Total: 10},
			Compressed: phaseProgress{Current: 10, Total: 10},
			Objects:    phaseProgress{Current: 10, Total: 10},
			Deltas:     phaseProgress{Current: 10, Total: 10},
			Files:      phaseProgress{Current: 2, Total: 10},
		},
	}

	current, total := p.DisplayState(0)
	require.Equal(t, cloneDisplayTotal-checkoutWeight, total)
	require.Equal(t, pendingProgressValue(total), current)
}

func TestCloneProgressMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    cloneProgress
		want string
	}{
		{
			name: "default",
			want: "Cloning",
		},
		{
			name: "checkout",
			p: cloneProgress{
				Git: gitProgress{
					Files: phaseProgress{Current: 10, Total: 10},
				},
			},
			want: "Checking out",
		},
		{
			name: "checkout before files",
			p: cloneProgress{
				Git: gitProgress{
					Deltas: phaseProgress{Current: 10, Total: 10},
				},
			},
			want: "Checking out",
		},
		{
			name: "filtering",
			p: cloneProgress{
				Git: gitProgress{
					FilterContent: phaseProgress{Current: 1, Total: 10},
				},
			},
			want: "Checking out LFS",
		},
		{
			name: "lfs",
			p: cloneProgress{
				LFS: lfsProgress{CurrentFile: 1, TotalFiles: 2},
			},
			want: "Checking out LFS",
		},
		{
			name: "lfs download",
			p: cloneProgress{
				LFS: lfsProgress{Operation: "download", CurrentFile: 1, TotalFiles: 2},
			},
			want: "Downloading LFS",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, test.p.Message())
		})
	}
}

func TestReadLFSProgress(t *testing.T) {
	t.Parallel()

	progress, ok := readLFSProgress("checkout 5/10 50/100 assets/model.bin")
	require.True(t, ok)
	require.Equal(t, "checkout", progress.Operation)
	require.Equal(t, 5, progress.CurrentFile)
	require.Equal(t, 10, progress.TotalFiles)
	require.Equal(t, int64(50), progress.CurrentBytes)
	require.Equal(t, int64(100), progress.TotalBytes)
	require.Equal(t, "assets/model.bin", progress.Name)
}

func TestRelayLFSProgress(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "lfs-progress-*")
	require.NoError(t, err)
	path := file.Name()
	require.NoError(t, file.Close())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	progressCh := make(chan lfsProgress, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- relayLFSProgress(ctx, path, func(progress *lfsProgress) {
			progressCh <- *progress
		})
	}()

	writer, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	if _, err := writer.WriteString("checkout 5/10 50/100 assets/model.bin\n"); err != nil {
		_ = writer.Close()
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	select {
	case progress := <-progressCh:
		require.Equal(t, "assets/model.bin", progress.Name)
	case <-time.After(5 * lfsPollInterval):
		require.FailNow(t, "timed out waiting for LFS progress")
	}

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(5 * lfsPollInterval):
		require.FailNow(t, "timed out waiting for relayLFSProgress to stop")
	}
}

func TestReadUntilCRLF(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(
		strings.NewReader("Receiving objects: 1% (1/100)\rChecking connectivity\nfatal: boom"),
	)

	line, err := readUntilCRLF(reader)
	require.NoError(t, err)
	require.Equal(t, "Receiving objects: 1% (1/100)\r", line)

	line, err = readUntilCRLF(reader)
	require.NoError(t, err)
	require.Equal(t, "Checking connectivity\n", line)

	line, err = readUntilCRLF(reader)
	require.NoError(t, err)
	require.Equal(t, "fatal: boom", line)
}

func TestTrimSidebandLine(t *testing.T) {
	t.Parallel()

	line, term := trimSidebandLine("remote: hello   \r")
	require.Equal(t, "remote: hello", line)
	require.NotNil(t, term)
	require.Equal(t, sidebandCR, *term)

	line, term = trimSidebandLine("local message\n")
	require.Equal(t, "local message", line)
	require.NotNil(t, term)
	require.Equal(t, sidebandLF, *term)

	line, term = trimSidebandLine("no terminator")
	require.Equal(t, "no terminator", line)
	require.Nil(t, term)
}

func TestIsErrorLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want bool
	}{
		{"fatal: Could not read from remote repository.", true},
		{"error: remote-tracking branch 'foo' not found", true},
		{"remote: Enumerating objects: 42, done.", false},
		{"Receiving objects: 50% (5/10)", false},
	}

	for _, test := range tests {
		assert.Equal(t, test.want, isErrorLine(test.line), "isErrorLine(%q)", test.line)
	}
}

func TestRelayGitProgressErrorCapture(t *testing.T) {
	t.Parallel()

	input := "remote: Counting objects: 100% (5/5), done.\r" +
		"Receiving objects: 50% (3/6)\r" +
		"fatal: Could not read from remote repository.\n" +
		"Please make sure you have the correct access rights\n"

	cb := &testCallback{}
	errText, err := relayGitProgress(strings.NewReader(input), cb)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(errText, "fatal: "))
	require.NotZero(t, cb.progressCalls)
}

func TestRelayGitProgressSideband(t *testing.T) {
	t.Parallel()

	input := "remote: GitHub found 2 vulnerabilities\n" +
		"Cloning into 'repo'...\n" +
		"Receiving objects: 100% (10/10)\r"

	cb := &testCallback{}
	errText, err := relayGitProgress(strings.NewReader(input), cb)
	require.NoError(t, err)
	require.Empty(t, errText)
	require.Equal(t, 1, cb.remoteCalls)
	require.Equal(t, 1, cb.localCalls)
	require.Equal(t, 1, cb.progressCalls)
}

type testCallback struct {
	progressCalls int
	localCalls    int
	remoteCalls   int
}

func (c *testCallback) Progress(*gitProgress)                      { c.progressCalls++ }
func (c *testCallback) LocalSideband(string, *sidebandTerminator)  { c.localCalls++ }
func (c *testCallback) RemoteSideband(string, *sidebandTerminator) { c.remoteCalls++ }
