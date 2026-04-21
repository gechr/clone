package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gechr/x/human"
)

// rangeFilter is an inclusive [min, max] integer range. Zero on either side
// means unbounded on that side - the zero value matches everything, which
// suits filters like star counts where min:0 is a no-op anyway.
type rangeFilter struct {
	min int
	max int
}

func (r rangeFilter) present() bool { return r.min > 0 || r.max > 0 }

func (r rangeFilter) matches(n int) bool {
	if r.min > 0 && n < r.min {
		return false
	}
	if r.max > 0 && n > r.max {
		return false
	}
	return true
}

// parseRangeFilter parses a star-style filter expression. Supported forms:
//
//	N            (at least N)
//	>N, >=N      (strictly more than / at least N)
//	<N, <=N      (strictly less than / at most N)
//	=N           (exactly N)
//	N..M, N-M    (inclusive range)
func parseRangeFilter(expr string) (rangeFilter, error) {
	s := strings.TrimSpace(expr)
	if s == "" {
		return rangeFilter{}, fmt.Errorf("empty range filter")
	}
	for _, sep := range []string{"..", "-"} {
		before, after, ok := strings.Cut(s, sep)
		if !ok {
			continue
		}
		lo, errLo := strconv.Atoi(before)
		hi, errHi := strconv.Atoi(after)
		if errLo != nil || errHi != nil {
			continue
		}
		if lo < 0 || hi < lo {
			return rangeFilter{}, fmt.Errorf("invalid range %q", expr)
		}
		return rangeFilter{min: lo, max: hi}, nil
	}

	parseInt := func(body string) (int, error) {
		n, err := strconv.Atoi(body)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid range %q", expr)
		}
		return n, nil
	}
	switch {
	case strings.HasPrefix(s, ">="):
		n, err := parseInt(s[2:])
		if err != nil {
			return rangeFilter{}, err
		}
		return rangeFilter{min: n}, nil
	case strings.HasPrefix(s, "<="):
		n, err := parseInt(s[2:])
		if err != nil {
			return rangeFilter{}, err
		}
		return rangeFilter{max: n}, nil
	case strings.HasPrefix(s, ">"):
		n, err := parseInt(s[1:])
		if err != nil {
			return rangeFilter{}, err
		}
		return rangeFilter{min: n + 1}, nil
	case strings.HasPrefix(s, "<"):
		n, err := parseInt(s[1:])
		if err != nil {
			return rangeFilter{}, err
		}
		if n == 0 {
			return rangeFilter{}, fmt.Errorf("invalid range %q (would match nothing)", expr)
		}
		return rangeFilter{max: n - 1}, nil
	case strings.HasPrefix(s, "="):
		n, err := parseInt(s[1:])
		if err != nil {
			return rangeFilter{}, err
		}
		return rangeFilter{min: n, max: n}, nil
	default:
		n, err := parseInt(s)
		if err != nil {
			return rangeFilter{}, err
		}
		return rangeFilter{min: n}, nil
	}
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }
func isLower(r rune) bool { return r >= 'a' && r <= 'z' }
func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

func compactLines(text string) string {
	lines := strings.Split(text, "\n")
	parts := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		parts = append(parts, line)
	}
	return strings.Join(parts, " | ")
}

// detectVCS inspects an existing clone to decide whether to drive it with jj
// or git. A `.jj` directory takes precedence - colocated repos have both and
// jj should own the update. Falls back to the caller's requested VCS when
// neither marker is present.
func detectVCS(dest, fallback string) string {
	if ok, _ := pathExists(filepath.Join(dest, ".jj")); ok {
		return vcsJJ
	}
	if ok, _ := pathExists(filepath.Join(dest, dotGit)); ok {
		return vcsGit
	}
	return fallback
}

// formatCommand formats a command as a shell-quoted string. When dry is true,
// $HOME-prefixed paths are contracted to ~ for readability. Dry output MUST
// NOT be executed - the ~ tokens aren't expanded by exec.
func formatCommand(bin string, args []string, dry bool) string {
	render := shellQuote
	if dry {
		render = func(s string) string { return shellQuote(human.ContractHome(s)) }
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, render(bin))
	for _, arg := range args {
		parts = append(parts, render(arg))
	}
	return strings.Join(parts, " ")
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

func runCommandInDir(ctx context.Context, dir, bin string, args []string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return formatCloneError(err, strings.TrimSpace(string(output)))
	}
	return nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
