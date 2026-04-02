package main

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"
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
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v", ok, test.ok)
			}
			if current != test.current || total != test.total {
				t.Fatalf("got (%d, %d), want (%d, %d)", current, total, test.current, test.total)
			}
		})
	}
}

func TestGitProgressApply(t *testing.T) {
	t.Parallel()

	progress := gitProgress{}

	if !progress.apply("remote: Counting objects: 100% (10/10), done.") {
		t.Fatal("apply(counting) = false")
	}
	if !progress.apply("Receiving objects:  50% (5/10), 1.23 MiB | 1.23 MiB/s") {
		t.Fatal("apply(receiving) = false")
	}
	if !progress.apply("Resolving deltas: 100% (3/3), done.") {
		t.Fatal("apply(resolving) = false")
	}
	if !progress.apply("Updating files: 100% (20/20), done.") {
		t.Fatal("apply(updating) = false")
	}
	if !progress.apply("Filtering content: 100% (1/1), 233.51 MiB | 6.14 MiB/s, done.") {
		t.Fatal("apply(filtering) = false")
	}
	if progress.apply("remote: Enumerating objects: 42, done.") {
		t.Fatal("apply(enumerating) = true, want false")
	}

	if got, want := progress.Current(), 39; got != want {
		t.Fatalf("Current() = %d, want %d", got, want)
	}
	if got, want := progress.Total(), 44; got != want {
		t.Fatalf("Total() = %d, want %d", got, want)
	}
}

func TestGitProgressOverall(t *testing.T) {
	t.Parallel()

	p := gitProgress{}
	if got := p.Overall(); got != 0 {
		t.Fatalf("Overall() = %f, want 0", got)
	}

	p.Objects = phaseProgress{Current: 50, Total: 100}
	if got, want := p.Overall(), 0.5; got != want {
		t.Fatalf("Overall() = %f, want %f", got, want)
	}
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

			if got := test.p.DisplayCurrent(); got != test.want {
				t.Fatalf("DisplayCurrent() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCloneProgressDisplayTotal(t *testing.T) {
	t.Parallel()

	p := cloneProgress{}
	if got, want := p.DisplayTotal(), cloneDisplayTotal-filesWeight-checkoutWeight; got != want {
		t.Fatalf("DisplayTotal() = %d, want %d", got, want)
	}

	p.Git.Files = phaseProgress{Current: 1, Total: 2}
	if got, want := p.DisplayTotal(), cloneDisplayTotal-checkoutWeight; got != want {
		t.Fatalf("DisplayTotal() with files = %d, want %d", got, want)
	}

	p.LFS = lfsProgress{CurrentFile: 1, TotalFiles: 2}
	if got, want := p.DisplayTotal(), cloneDisplayTotal; got != want {
		t.Fatalf("DisplayTotal() with LFS = %d, want %d", got, want)
	}
}

func TestCloneProgressDisplayStateCapsIncompleteTotal(t *testing.T) {
	t.Parallel()

	p := cloneProgress{
		Git: gitProgress{
			Deltas: phaseProgress{Current: 10, Total: 10},
		},
	}

	current, total := p.DisplayState(0)
	if want := cloneDisplayTotal - filesWeight - checkoutWeight; total != want {
		t.Fatalf("DisplayState() total = %d, want %d", total, want)
	}
	if current >= total {
		t.Fatalf("DisplayState() current = %d, want less than total %d", current, total)
	}
	if want := pendingProgressValue(total); current != want {
		t.Fatalf("DisplayState() current = %d, want %d", current, want)
	}
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
	if want := cloneDisplayTotal - checkoutWeight; total != want {
		t.Fatalf("DisplayState() total = %d, want %d", total, want)
	}
	if want := pendingProgressValue(total); current != want {
		t.Fatalf("DisplayState() current = %d, want %d", current, want)
	}
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

			if got := test.p.Message(); got != test.want {
				t.Fatalf("Message() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReadLFSProgress(t *testing.T) {
	t.Parallel()

	progress, ok := readLFSProgress("checkout 5/10 50/100 assets/model.bin")
	if !ok {
		t.Fatal("readLFSProgress() = false")
	}
	if progress.Operation != "checkout" {
		t.Fatalf("Operation = %q, want %q", progress.Operation, "checkout")
	}
	if progress.CurrentFile != 5 || progress.TotalFiles != 10 {
		t.Fatalf("file counts = (%d, %d), want (5, 10)", progress.CurrentFile, progress.TotalFiles)
	}
	if progress.CurrentBytes != 50 || progress.TotalBytes != 100 {
		t.Fatalf(
			"byte counts = (%d, %d), want (50, 100)",
			progress.CurrentBytes,
			progress.TotalBytes,
		)
	}
	if progress.Name != "assets/model.bin" {
		t.Fatalf("Name = %q, want %q", progress.Name, "assets/model.bin")
	}
}

func TestRelayLFSProgress(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "lfs-progress-*")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	path := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}

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
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := writer.WriteString("checkout 5/10 50/100 assets/model.bin\n"); err != nil {
		_ = writer.Close()
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case progress := <-progressCh:
		if progress.Name != "assets/model.bin" {
			t.Fatalf("Name = %q, want %q", progress.Name, "assets/model.bin")
		}
	case <-time.After(5 * lfsPollInterval):
		t.Fatal("timed out waiting for LFS progress")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("relayLFSProgress() error = %v", err)
		}
	case <-time.After(5 * lfsPollInterval):
		t.Fatal("timed out waiting for relayLFSProgress to stop")
	}
}

func TestReadUntilCRLF(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(
		strings.NewReader("Receiving objects: 1% (1/100)\rChecking connectivity\nfatal: boom"),
	)

	line, err := readUntilCRLF(reader)
	if err != nil {
		t.Fatalf("readUntilCRLF() error = %v", err)
	}
	if got, want := line, "Receiving objects: 1% (1/100)\r"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}

	line, err = readUntilCRLF(reader)
	if err != nil {
		t.Fatalf("readUntilCRLF() error = %v", err)
	}
	if got, want := line, "Checking connectivity\n"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}

	line, err = readUntilCRLF(reader)
	if err != nil {
		t.Fatalf("readUntilCRLF() error = %v", err)
	}
	if got, want := line, "fatal: boom"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

func TestTrimSidebandLine(t *testing.T) {
	t.Parallel()

	line, term := trimSidebandLine("remote: hello   \r")
	if got, want := line, "remote: hello"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	if term == nil || *term != sidebandCR {
		t.Fatalf("term = %v, want CR", term)
	}

	line, term = trimSidebandLine("local message\n")
	if got, want := line, "local message"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	if term == nil || *term != sidebandLF {
		t.Fatalf("term = %v, want LF", term)
	}

	line, term = trimSidebandLine("no terminator")
	if got, want := line, "no terminator"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	if term != nil {
		t.Fatalf("term = %v, want nil", term)
	}
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
		if got := isErrorLine(test.line); got != test.want {
			t.Errorf("isErrorLine(%q) = %v, want %v", test.line, got, test.want)
		}
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
	if err != nil {
		t.Fatalf("relayGitProgress() error = %v", err)
	}

	if !strings.HasPrefix(errText, "fatal: ") {
		t.Fatalf("errText = %q, want prefix 'fatal: '", errText)
	}
	if cb.progressCalls == 0 {
		t.Fatal("expected progress callbacks before error")
	}
}

func TestRelayGitProgressSideband(t *testing.T) {
	t.Parallel()

	input := "remote: GitHub found 2 vulnerabilities\n" +
		"Cloning into 'repo'...\n" +
		"Receiving objects: 100% (10/10)\r"

	cb := &testCallback{}
	errText, err := relayGitProgress(strings.NewReader(input), cb)
	if err != nil {
		t.Fatalf("relayGitProgress() error = %v", err)
	}

	if errText != "" {
		t.Fatalf("errText = %q, want empty", errText)
	}
	if cb.remoteCalls != 1 {
		t.Fatalf("remote sideband calls = %d, want 1", cb.remoteCalls)
	}
	if cb.localCalls != 1 {
		t.Fatalf("local sideband calls = %d, want 1", cb.localCalls)
	}
	if cb.progressCalls != 1 {
		t.Fatalf("progress calls = %d, want 1", cb.progressCalls)
	}
}

type testCallback struct {
	progressCalls int
	localCalls    int
	remoteCalls   int
}

func (c *testCallback) Progress(*gitProgress)                      { c.progressCalls++ }
func (c *testCallback) LocalSideband(string, *sidebandTerminator)  { c.localCalls++ }
func (c *testCallback) RemoteSideband(string, *sidebandTerminator) { c.remoteCalls++ }
