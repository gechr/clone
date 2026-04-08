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

type fetchTask struct {
	fetcher *Fetcher
	result  *fx.TaskResult
}

type Fetcher struct {
	BinGit  string
	BinJJ   string
	Dest    string
	Label   string
	RepoURL string
	Slug    string
	VCS     string

	Done bool
	Err  error
}

func NewFetcher(target CloneTarget) *Fetcher {
	return &Fetcher{
		BinGit:  target.BinGit,
		BinJJ:   target.BinJJ,
		Dest:    target.Dest,
		Label:   target.Label,
		RepoURL: target.RepoURL,
		Slug:    target.Slug,
		VCS:     target.VCS,
	}
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

	Err error
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
		if p.Transferring {
			c.transferStats.Store(&p.Transfer)
		} else {
			c.transferStats.Store(nil)
		}
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

func groupFooterLabel(cloners []*Cloner, fetchers []*Fetcher) (string, string) {
	hasClones := len(cloners) > 0
	hasFetches := len(fetchers) > 0
	switch {
	case hasClones && hasFetches:
		return "Syncing", "Synced"
	case hasFetches:
		return "Fetching", "Fetched"
	default:
		return "Cloning", "Cloned"
	}
}

func groupOptions(parallelism, taskCount int, activeLabel, doneLabel string) []clog.GroupOption {
	options := []clog.GroupOption{
		clog.WithParallelism(parallelism),
		clog.WithHideDone(),
		clog.WithMaxHeightPercent(0.5), //nolint:mnd // half the terminal window
	}
	if showOverallProgress(taskCount) {
		options = append(options, clog.WithFooter(
			clog.Spinner(activeLabel),
			func(done, total int, u *clog.Update) {
				msg := activeLabel
				if done == total {
					msg = doneLabel
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

func logCloneFailed(failed []*Cloner) {
	links := cloneLinks(failed)
	if len(links) == 1 {
		e := clog.Error().Link("repository", links[0].URL, links[0].Text)
		if failed[0].Err != nil {
			e = e.Str("reason", failed[0].Err.Error())
		}
		e.Msg("Clone failed")
		return
	}
	e := clog.Error().
		Links("repositories", links).
		Int("total", len(links))
	for _, c := range failed {
		if c.Err != nil {
			e = e.Str(c.Label, c.Err.Error())
		}
	}
	e.Msg("Clone failed")
}

func logCloneResult(baseDir string, all, failed []*Cloner) {
	if len(failed) > 0 {
		logCloneFailed(failed)
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
		logCloneSucceeded(baseDir, succeeded)
	}
}

func logCloneSucceeded(baseDir string, succeeded []*Cloner) {
	links := cloneLinks(succeeded)
	if len(links) == 1 {
		e := clog.Log(LevelSuccess).Link("repository", links[0].URL, links[0].Text)
		if baseDir != "" {
			e = e.Path("directory", succeeded[0].Dest)
		}
		e.Msg("Cloned")
	} else {
		e := clog.Log(LevelSuccess).
			Links("repositories", links)
		if baseDir != "" {
			e = e.Path("directory", baseDir)
		}
		e.Int("total", len(links)).Msg("Cloned")
	}
}

func fetchLinks(fetchers []*Fetcher) []clog.Link {
	links := make([]clog.Link, len(fetchers))
	for i, f := range fetchers {
		links[i] = clog.Link{Text: f.Label, URL: f.RepoURL}
	}
	return links
}

func logFetchFailed(failed []*Fetcher) {
	links := fetchLinks(failed)
	if len(links) == 1 {
		e := clog.Error().Link("repository", links[0].URL, links[0].Text)
		if failed[0].Err != nil {
			e = e.Str("reason", failed[0].Err.Error())
		}
		e.Msg("Fetch failed")
		return
	}
	e := clog.Error().
		Links("repositories", links).
		Int("total", len(links))
	for _, f := range failed {
		if f.Err != nil {
			e = e.Str(f.Label, f.Err.Error())
		}
	}
	e.Msg("Fetch failed")
}

func logFetchResult(all, failed []*Fetcher) {
	if len(failed) > 0 {
		logFetchFailed(failed)
	}

	if len(all) > len(failed) {
		failedSet := make(map[*Fetcher]struct{}, len(failed))
		for _, f := range failed {
			failedSet[f] = struct{}{}
		}
		succeeded := make([]*Fetcher, 0, len(all)-len(failed))
		for _, f := range all {
			if _, ok := failedSet[f]; !ok {
				succeeded = append(succeeded, f)
			}
		}
		links := fetchLinks(succeeded)
		if len(links) == 1 {
			clog.Log(LevelSuccess).Link("repository", links[0].URL, links[0].Text).Msg("Fetched")
		} else {
			clog.Log(LevelSuccess).
				Links("repositories", links).
				Int("total", len(links)).
				Msg("Fetched")
		}
	}
}

func executeClones(ctx context.Context, cli *CLI, baseDir string, targets []CloneTarget) error {
	cloners, fetchers, err := prepareCloners(targets, !cli.Quiet, !cli.Dry, cli.Force, cli.Fetch)
	if err != nil {
		return err
	}
	if len(cloners) == 0 && len(fetchers) == 0 {
		return nil
	}

	if cli.Dry {
		for _, cloner := range cloners {
			clog.Dry().Msg(cloner.DryRunCommand())
		}
		for _, fetcher := range fetchers {
			clog.Dry().Msg(fetcher.DryRunCommand())
		}
		return nil
	}

	var execErr error
	if cli.Quiet {
		execErr = executeQuiet(ctx, baseDir, cloners, fetchers, cli.Parallelism)
	} else {
		execErr = executeWithProgress(ctx, baseDir, cloners, fetchers, cli.Parallelism, cli.Verbose)
	}

	if execErr != nil {
		cleanupIncompleteClones(cloners)
	}

	return execErr
}

func prepareCloners(
	targets []CloneTarget,
	warn bool,
	createParents bool,
	force bool,
	fetch bool,
) ([]*Cloner, []*Fetcher, error) {
	cloners := make([]*Cloner, 0, len(targets))
	var fetchers []*Fetcher
	var skipped []CloneTarget
	for _, target := range targets {
		if createParents {
			if err := ensureCloneParent(target.Dest); err != nil {
				return nil, nil, err
			}
		}
		exists, err := pathExists(target.Dest)
		if err != nil {
			return nil, nil, err
		}
		if exists && fetch {
			fetchers = append(fetchers, NewFetcher(target))
			continue
		}
		if exists && !force {
			skipped = append(skipped, target)
			continue
		}
		if exists {
			if err := os.RemoveAll(target.Dest); err != nil {
				return nil, nil, fmt.Errorf("removing existing clone %s: %w", target.Dest, err)
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
	return cloners, fetchers, nil
}

func executeQuiet(
	ctx context.Context,
	baseDir string,
	cloners []*Cloner,
	fetchers []*Fetcher,
	parallelism int,
) error {
	if parallelism < 1 {
		parallelism = 1
	}

	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failedClones []*Cloner
	var failedFetches []*Fetcher
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
				cloner.Err = err
				mu.Lock()
				failedClones = append(failedClones, cloner)
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}

	for _, fetcher := range fetchers {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			if err := fetcher.Run(ctx, nil, nil); err != nil {
				fetcher.Err = err
				mu.Lock()
				failedFetches = append(failedFetches, fetcher)
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}

	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	logCloneResult(baseDir, cloners, failedClones)
	logFetchResult(fetchers, failedFetches)
	return errors.Join(errs...)
}

func executeWithProgress(
	ctx context.Context,
	baseDir string,
	cloners []*Cloner,
	fetchers []*Fetcher,
	parallelism int,
	verbose bool,
) error {
	taskCount := len(cloners) + len(fetchers)
	activeLabel, doneLabel := groupFooterLabel(cloners, fetchers)
	group := clog.Group(ctx, groupOptions(parallelism, taskCount, activeLabel, doneLabel)...)

	cloneTasks := make([]cloneTask, 0, len(cloners))
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
		cloneTasks = append(cloneTasks, cloneTask{
			cloner: cloner,
			result: result,
		})
	}

	fTasks := make([]fetchTask, 0, len(fetchers))
	for _, fetcher := range fetchers {
		stats := &atomic.Pointer[transferStats]{}
		b := clog.Bar(
			"Fetching",
			1,
			cloneBarOptions(verbose, stats)...).
			Symbol("·").
			Spinner().
			Link("repository", fetcher.RepoURL, fetcher.Label)
		result := group.Add(b).Progress(func(ctx context.Context, update *clog.Update) error {
			return fetcher.Run(ctx, update, stats)
		})
		fTasks = append(fTasks, fetchTask{
			fetcher: fetcher,
			result:  result,
		})
	}

	waitErr := group.Wait().Silent()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	var failedClones []*Cloner
	var failedFetches []*Fetcher
	var errs []error
	for _, task := range cloneTasks {
		if err := task.result.Silent(); err != nil {
			task.cloner.Err = err
			failedClones = append(failedClones, task.cloner)
			errs = append(errs, err)
		}
	}
	for _, task := range fTasks {
		if err := task.result.Silent(); err != nil {
			task.fetcher.Err = err
			failedFetches = append(failedFetches, task.fetcher)
			errs = append(errs, err)
		}
	}
	if waitErr != nil && len(errs) == 0 {
		errs = append(errs, waitErr)
	}

	logCloneResult(baseDir, cloners, failedClones)
	logFetchResult(fetchers, failedFetches)
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
			update.Msg("Clone failed").
				SetSymbol("✘").
				SetLevel(clog.LevelError).
				Str("reason", err.Error()).
				Send()
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

	if err := runCommandInDir(ctx, c.Dest, c.BinGit, []string{
		"fetch", "origin", prRef, "--no-tags", "--quiet",
	}); err != nil {
		return fmt.Errorf("fetching PR #%s: %w", c.PullRequest, err)
	}

	if c.VCS == vcsJJ {
		if err := runCommandInDir(ctx, c.Dest, c.BinJJ, []string{"git", "import"}); err != nil {
			return fmt.Errorf("importing git refs: %w", err)
		}
		return runCommandInDir(ctx, c.Dest, c.BinJJ, []string{"new", c.PRHeadRef, "--quiet"})
	}

	return runCommandInDir(ctx, c.Dest, c.BinGit, []string{"checkout", c.PRHeadRef, "--quiet"})
}

func (c *Cloner) runJJClone(
	ctx context.Context,
	update *clog.Update,
	stats *atomic.Pointer[transferStats],
) error {
	if err := c.runGitClone(ctx, update, stats); err != nil {
		return err
	}
	return runCommandInDir(ctx, c.Dest, c.BinJJ, c.jjInitArgs())
}

func (c *Cloner) runGitClone(
	ctx context.Context,
	update *clog.Update,
	stats *atomic.Pointer[transferStats],
) error {
	args := c.gitCloneArgs(update != nil)
	if update == nil {
		return runCommandInDir(ctx, "", c.BinGit, args)
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

func runCommandInDir(ctx context.Context, dir, bin string, args []string) error {
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

func (f *Fetcher) Run(
	ctx context.Context,
	update *clog.Update,
	stats *atomic.Pointer[transferStats],
) error {
	if update != nil {
		update.SetTotal(1).SetProgress(0).Send()
	}

	var err error
	switch f.VCS {
	case vcsJJ:
		err = f.runJJFetch(ctx, update, stats)
	default:
		err = f.runGitFetch(ctx, update, stats)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if update != nil {
			update.Msg("Fetch failed").
				SetSymbol("✘").
				SetLevel(clog.LevelError).
				Str("reason", err.Error()).
				Send()
		}
		return err
	}

	f.Done = true
	if update != nil {
		update.Msg("Fetched").
			SetSymbol("✔︎").
			SetLevel(LevelSuccess).
			SetTotal(1).
			SetProgress(1).
			Send()
	}
	return nil
}

func (f *Fetcher) runGitFetch(
	ctx context.Context,
	update *clog.Update,
	stats *atomic.Pointer[transferStats],
) error {
	args := f.gitFetchArgs(update != nil)
	if update == nil {
		return runCommandInDir(ctx, "", f.BinGit, args)
	}

	//nolint:gosec // resolved via LookPath
	cmd := exec.CommandContext(ctx, f.BinGit, args...)
	cmd.Stdout = io.Discard

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	cb := &cloneCallback{update: update, transferStats: stats}
	stderrText, parseErr := relayGitProgress(stderr, cb)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return parseErr
	}
	if waitErr != nil {
		return formatCloneError(waitErr, stderrText)
	}
	return nil
}

func (f *Fetcher) runJJFetch(
	ctx context.Context,
	update *clog.Update,
	stats *atomic.Pointer[transferStats],
) error {
	if err := f.runGitFetch(ctx, update, stats); err != nil {
		return err
	}
	return runCommandInDir(ctx, "", f.BinJJ, f.jjImportArgs())
}

func (f *Fetcher) gitFetchArgs(includeProgress bool) []string {
	args := []string{"-C", f.Dest, "fetch"}
	if includeProgress {
		args = append(args, "--progress")
	}
	return args
}

func (f *Fetcher) jjImportArgs() []string {
	return []string{"-R", f.Dest, "git", "import", "--quiet"}
}

func (f *Fetcher) DryRunCommand() string {
	lines := []string{formatCommand(f.BinGit, f.gitFetchArgs(false))}
	if f.VCS == vcsJJ {
		lines = append(lines, formatCommand(f.BinJJ, f.jjImportArgs()))
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
