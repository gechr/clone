package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gechr/clib/human"
	"github.com/gechr/clog"
	"github.com/gechr/clog/fx"
	"github.com/gechr/clog/fx/bar"
	"github.com/gechr/clog/fx/bar/widget"
)

const minOverallProgressRepos = 5

type cloneTask struct {
	cloner *Cloner
	result *fx.TaskResult
}

type Cloner struct {
	BinGit       string
	BinJJ        string
	Branch       string
	CustomDest   bool
	Depth        int
	Dest         string
	Done         bool
	Label        string
	Mirror       bool
	PRHeadRef    string
	PullRequest  string
	RepoURL      string
	SingleBranch bool
	Slug         string
	Source       string
	VCS          string
}

func NewCloner(target CloneTarget) *Cloner {
	return &Cloner{
		BinGit:       target.BinGit,
		BinJJ:        target.BinJJ,
		Branch:       target.Branch,
		CustomDest:   target.CustomDest,
		Depth:        target.Depth,
		Dest:         target.Dest,
		Label:        target.Label,
		Mirror:       target.Mirror,
		PRHeadRef:    target.PRHeadRef,
		PullRequest:  target.PullRequest,
		RepoURL:      target.RepoURL,
		SingleBranch: target.SingleBranch,
		Slug:         target.Slug,
		Source:       target.Source,
		VCS:          target.VCS,
	}
}

type cloneCallback struct {
	mu            sync.Mutex
	update        *clog.Update
	progress      cloneProgress
	lastProgress  int
	transferStats *atomic.Pointer[transferStats] // shared with widget; nil when not verbose
}

func (c *cloneCallback) send() {
	if c.update == nil {
		return
	}
	c.update.Send()
}

func (c *cloneCallback) Progress(p *gitProgress) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.progress.Git = *p
	if c.transferStats != nil {
		c.transferStats.Store(&p.Transfer)
	}
	c.sendProgressLocked()
}

func (c *cloneCallback) LFSProgress(p *lfsProgress) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.progress.LFS = *p
	c.sendProgressLocked()
}

func (c *cloneCallback) sendProgressLocked() {
	current, total := c.progress.DisplayState(c.lastProgress)
	c.lastProgress = current

	c.update.Msg(c.progress.Message()).
		SetTotal(total).
		SetProgress(current)
	c.send()
}

func (c *cloneCallback) LocalSideband(string, *sidebandTerminator)  {}
func (c *cloneCallback) RemoteSideband(string, *sidebandTerminator) {}

const transferStatsDelay = 10 * time.Second

func cloneBarOptions(verbose bool, stats *atomic.Pointer[transferStats]) []bar.Option {
	percentWidget := widget.Percent(
		widget.WithMinimumPercent(1),
		widget.WithProgressGradient(bar.DefaultGradient()...),
	)
	dim := new(lipgloss.NewStyle().Faint(true))
	rightWidget := widget.Widgets(
		percentWidget,
		transferStatsWidget(stats, dim, verbose),
	)

	return []bar.Option{
		bar.WithStyle(bar.Thin),
		bar.WithPendingMode(bar.PendingHide),
		bar.WithProgressGradient(bar.DefaultGradient()...),
		bar.WithWidgetLeft(widget.None()),
		bar.WithWidgetRight(rightWidget),
		bar.WithMaxWidth(15), //nolint:mnd // bar width
		bar.WithPlacement(bar.PlaceAligned),
	}
}

func transferStatsWidget(
	stats *atomic.Pointer[transferStats],
	style *lipgloss.Style,
	verbose bool,
) bar.Widget {
	return func(s bar.State) string {
		if !verbose && s.Elapsed < transferStatsDelay {
			return ""
		}
		p := stats.Load()
		if p == nil || (p.Bytes == 0 && p.Speed == 0) {
			return ""
		}
		var parts []string
		if p.Bytes > 0 {
			parts = append(parts, human.FormatIECBytes(p.Bytes))
		}
		if p.Speed > 0 {
			parts = append(parts, human.FormatIECBytes(p.Speed)+"/s")
		}
		if len(parts) == 0 {
			return ""
		}
		text := "(" + strings.Join(parts, ", ") + ")"
		if style != nil {
			return style.Render(text)
		}
		return text
	}
}

func showOverallProgress(repoCount int) bool {
	return repoCount >= minOverallProgressRepos
}

func cloneGroupOptions(parallelism, repoCount int) []clog.GroupOption {
	options := []clog.GroupOption{
		clog.WithParallelism(parallelism),
		clog.WithHideDone(),
		clog.WithMaxHeightPercent(0.5), //nolint:mnd // half the terminal window
	}
	if showOverallProgress(repoCount) {
		options = append(options, clog.WithFooter(
			clog.Spinner("Cloning"),
			func(done, total int, u *clog.Update) {
				msg := "Cloning"
				if done == total {
					msg = "Cloned"
				}
				u.Msg(msg).Fraction("progress", done, total).Send()
			},
		))
	}

	return options
}

func cloneLinks(cloners []*Cloner) []clog.Link {
	links := make([]clog.Link, len(cloners))
	for i, c := range cloners {
		link := clog.Link{Text: c.Label, URL: c.RepoURL}
		links[i] = link
	}
	return links
}

func logCloneResult(all, failed []*Cloner) {
	if len(failed) > 0 {
		links := cloneLinks(failed)
		if len(links) == 1 {
			clog.Error().Link("repository", links[0].URL, links[0].Text).Msg("Clone failed")
		} else {
			clog.Error().
				Links("repositories", links).
				Int("total", len(links)).
				Msg("Clone failed")
		}
	}

	if len(all) > len(failed) {
		failedSet := make(map[*Cloner]struct{}, len(failed))
		for _, c := range failed {
			failedSet[c] = struct{}{}
		}
		succeeded := make([]*Cloner, 0, len(all)-len(failed))
		for _, c := range all {
			if _, ok := failedSet[c]; !ok {
				succeeded = append(succeeded, c)
			}
		}
		links := cloneLinks(succeeded)
		if len(links) == 1 {
			clog.Log(LevelSuccess).Link("repository", links[0].URL, links[0].Text).Msg("Cloned")
		} else {
			clog.Log(LevelSuccess).
				Links("repositories", links).
				Int("total", len(links)).
				Msg("Cloned")
		}
	}
}

func executeClones(ctx context.Context, cli *CLI, targets []CloneTarget) error {
	cloners, err := prepareCloners(targets, !cli.Quiet, !cli.Dry, cli.Force)
	if err != nil {
		return err
	}
	if len(cloners) == 0 {
		return nil
	}

	if cli.Dry {
		for _, cloner := range cloners {
			clog.Dry().Msg(cloner.DryRunCommand())
		}
		return nil
	}

	var cloneErr error
	if cli.Quiet {
		cloneErr = cloneQuiet(ctx, cloners, cli.Parallelism)
	} else {
		cloneErr = cloneWithProgress(ctx, cloners, cli.Parallelism, cli.Verbose)
	}

	if cloneErr != nil {
		cleanupIncompleteClones(cloners)
	}

	return cloneErr
}

func prepareCloners(
	targets []CloneTarget,
	warn bool,
	createParents bool,
	force bool,
) ([]*Cloner, error) {
	cloners := make([]*Cloner, 0, len(targets))
	var skipped []CloneTarget
	for _, target := range targets {
		if createParents {
			if err := ensureCloneParent(target.Dest); err != nil {
				return nil, err
			}
		}
		exists, err := pathExists(target.Dest)
		if err != nil {
			return nil, err
		}
		if exists && !force {
			skipped = append(skipped, target)
			continue
		}
		if exists {
			if err := os.RemoveAll(target.Dest); err != nil {
				return nil, fmt.Errorf("removing existing clone %s: %w", target.Dest, err)
			}
		}
		cloners = append(cloners, NewCloner(target))
	}
	if warn && len(skipped) > 0 {
		links := make([]clog.Link, len(skipped))
		for i, t := range skipped {
			links[i] = clog.Link{Text: t.Label, URL: t.RepoURL}
		}
		if len(links) == 1 {
			clog.Warn().Link("repository", links[0].URL, links[0].Text).Msg("Skipping")
		} else {
			clog.Warn().Links("repositories", links).Msg("Skipping")
		}
	}
	return cloners, nil
}

func cloneQuiet(ctx context.Context, cloners []*Cloner, parallelism int) error {
	if parallelism < 1 {
		parallelism = 1
	}

	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []*Cloner
	var errs []error

	for _, cloner := range cloners {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			if err := cloner.Run(ctx, nil, nil); err != nil {
				mu.Lock()
				failed = append(failed, cloner)
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}

	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	logCloneResult(cloners, failed)
	return errors.Join(errs...)
}

func cloneWithProgress(
	ctx context.Context,
	cloners []*Cloner,
	parallelism int,
	verbose bool,
) error {
	group := clog.Group(ctx, cloneGroupOptions(parallelism, len(cloners))...)
	tasks := make([]cloneTask, 0, len(cloners))

	for _, cloner := range cloners {
		stats := &atomic.Pointer[transferStats]{}
		b := clog.Bar(
			"Cloning",
			1,
			cloneBarOptions(verbose, stats)...).
			Symbol("·").
			Spinner().
			Link("repository", cloner.RepoURL, cloner.Label)
		if cloner.CustomDest {
			b = b.Path("destination", cloner.Dest)
		}
		result := group.Add(b).Progress(func(ctx context.Context, update *clog.Update) error {
			return cloner.Run(ctx, update, stats)
		})
		tasks = append(tasks, cloneTask{
			cloner: cloner,
			result: result,
		})
	}

	waitErr := group.Wait().Silent()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	var failed []*Cloner
	var errs []error
	for _, task := range tasks {
		if err := task.result.Silent(); err != nil {
			failed = append(failed, task.cloner)
			errs = append(errs, err)
		}
	}
	if waitErr != nil && len(errs) == 0 {
		errs = append(errs, waitErr)
	}

	logCloneResult(cloners, failed)
	return errors.Join(errs...)
}

func (c *Cloner) Run(
	ctx context.Context,
	update *clog.Update,
	stats *atomic.Pointer[transferStats],
) error {
	if update != nil {
		update.SetTotal(1).SetProgress(0).Send()
	}

	var err error
	switch c.VCS {
	case vcsJJ:
		err = c.runJJClone(ctx, update, stats)
	default:
		err = c.runGitClone(ctx, update, stats)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if update != nil {
			update.Msg("Clone failed").SetSymbol("✘").SetLevel(clog.LevelError).Send()
		}
		return err
	}

	if c.PullRequest != "" && c.PRHeadRef != "" {
		if prErr := c.checkoutPR(ctx); prErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return prErr
		}
	}

	c.Done = true
	if update != nil {
		update.Msg("Cloned").
			SetSymbol("✔︎").
			SetLevel(LevelSuccess).
			SetTotal(1).
			SetProgress(1).
			Send()
	}
	return nil
}

func (c *Cloner) checkoutPR(ctx context.Context) error {
	prRef := fmt.Sprintf("refs/pull/%s/head:%s", c.PullRequest, c.PRHeadRef)

	if err := c.runCommandInDir(ctx, c.Dest, c.BinGit, []string{
		"fetch", "origin", prRef, "--no-tags", "--quiet",
	}); err != nil {
		return fmt.Errorf("fetching PR #%s: %w", c.PullRequest, err)
	}

	if c.VCS == vcsJJ {
		if err := c.runCommandInDir(ctx, c.Dest, c.BinJJ, []string{"git", "import"}); err != nil {
			return fmt.Errorf("importing git refs: %w", err)
		}
		return c.runCommandInDir(ctx, c.Dest, c.BinJJ, []string{"new", c.PRHeadRef, "--quiet"})
	}

	return c.runCommandInDir(ctx, c.Dest, c.BinGit, []string{"checkout", c.PRHeadRef, "--quiet"})
}

func (c *Cloner) runJJClone(
	ctx context.Context,
	update *clog.Update,
	stats *atomic.Pointer[transferStats],
) error {
	if err := c.runGitClone(ctx, update, stats); err != nil {
		return err
	}
	return c.runCommandInDir(ctx, c.Dest, c.BinJJ, c.jjInitArgs())
}

func (c *Cloner) runGitClone(
	ctx context.Context,
	update *clog.Update,
	stats *atomic.Pointer[transferStats],
) error {
	args := c.gitCloneArgs(update != nil)
	if update == nil {
		return c.runCommandInDir(ctx, "", c.BinGit, args)
	}

	//nolint:gosec // resolved via LookPath
	cmd := exec.CommandContext(ctx, c.BinGit, args...)
	cmd.Stdout = io.Discard

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	progressFile, err := os.CreateTemp("", "clone-lfs-progress-*")
	if err != nil {
		return err
	}
	progressPath := progressFile.Name()
	if closeErr := progressFile.Close(); closeErr != nil {
		_ = os.Remove(progressPath)
		return closeErr
	}
	defer os.Remove(progressPath)

	cmd.Env = append(os.Environ(), "GIT_LFS_PROGRESS="+progressPath)

	if err := cmd.Start(); err != nil {
		return err
	}

	cb := &cloneCallback{update: update, transferStats: stats}
	lfsCtx, cancelLFS := context.WithCancel(ctx)
	defer cancelLFS()

	lfsErrCh := make(chan error, 1)
	go func() {
		lfsErrCh <- relayLFSProgress(lfsCtx, progressPath, cb.LFSProgress)
	}()

	stderrText, parseErr := relayGitProgress(stderr, cb)
	waitErr := cmd.Wait()
	cancelLFS()

	lfsErr := <-lfsErrCh
	switch {
	case parseErr != nil:
		return parseErr
	case lfsErr != nil:
		return lfsErr
	}
	if waitErr != nil {
		return formatCloneError(waitErr, stderrText)
	}
	return nil
}

func (c *Cloner) runCommandInDir(ctx context.Context, dir, bin string, args []string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return formatCloneError(err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (c *Cloner) gitCloneArgs(includeProgress bool) []string {
	args := []string{"clone"}
	if includeProgress {
		args = append(args, "--progress")
	}
	if c.SingleBranch {
		args = append(args, "--single-branch")
	}
	if c.Depth > 0 {
		args = append(args, "--depth="+strconv.Itoa(c.Depth))
	}
	if c.Branch != "" {
		args = append(args, "--branch", c.Branch)
	}
	if c.Mirror {
		args = append(args, "--mirror")
	}
	args = append(args, c.Source, c.Dest)
	return args
}

func (c *Cloner) jjInitArgs() []string {
	return []string{"git", "init", "--color=never", "--colocate", "."}
}

func (c *Cloner) DryRunCommand() string {
	lines := []string{formatCommand(c.BinGit, c.gitCloneArgs(false))}

	if c.VCS == vcsJJ {
		jjArgs := c.jjInitArgs()
		jjArgs[len(jjArgs)-1] = c.Dest
		lines = append(lines, formatCommand(c.BinJJ, jjArgs))
	}

	if c.PullRequest != "" && c.PRHeadRef != "" {
		prRef := fmt.Sprintf("refs/pull/%s/head:%s", c.PullRequest, c.PRHeadRef)
		lines = append(lines, formatCommand(c.BinGit, []string{
			"-C", c.Dest, "fetch", "origin", prRef, "--no-tags",
		}))
		if c.VCS == vcsJJ {
			lines = append(lines, formatCommand(c.BinJJ, []string{"-R", c.Dest, "git", "import"}))
			lines = append(
				lines,
				formatCommand(c.BinJJ, []string{"-R", c.Dest, "new", c.PRHeadRef}),
			)
		} else {
			lines = append(
				lines,
				formatCommand(c.BinGit, []string{"-C", c.Dest, "checkout", c.PRHeadRef}),
			)
		}
	}

	return strings.Join(lines, "\n")
}

func cleanupIncompleteClones(cloners []*Cloner) {
	for _, c := range cloners {
		if !c.Done {
			_ = os.RemoveAll(c.Dest)
		}
	}
}

func ensureCloneParent(dest string) error {
	parent := filepath.Dir(dest)
	if parent == "." || parent == "" {
		return nil
	}
	return os.MkdirAll(parent, 0o755)
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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func formatCommand(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(bin))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func formatCloneError(err error, stderrText string) error {
	if msg := classifyCloneError(stderrText); msg != "" {
		return errors.New(msg)
	}
	details := compactLines(stderrText)
	if details != "" {
		return errors.New(details)
	}
	return err
}

func classifyCloneError(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "repository not found"),
		strings.Contains(lower, "could not read from remote repository"):
		return "repository not found or insufficient permissions"
	case strings.Contains(lower, "could not resolve host"):
		return "could not resolve host"
	case strings.Contains(lower, "connection refused"):
		return "connection refused"
	case strings.Contains(lower, "permission denied"):
		return "permission denied"
	default:
		return ""
	}
}

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
