package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gechr/x/human"
)

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
// or git. A `.jj` directory takes precedence — colocated repos have both and
// jj should own the update. Falls back to the caller's requested VCS when
// neither marker is present.
func detectVCS(dest, fallback string) string {
	if ok, _ := pathExists(filepath.Join(dest, ".jj")); ok {
		return vcsJJ
	}
	if ok, _ := pathExists(filepath.Join(dest, ".git")); ok {
		return vcsGit
	}
	return fallback
}

// formatCommand formats a command as a shell-quoted string. When dry is true,
// $HOME-prefixed paths are contracted to ~ for readability. Dry output MUST
// NOT be executed — the ~ tokens aren't expanded by exec.
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
