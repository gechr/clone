package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/alecthomas/kong"
	"github.com/cli/go-gh/v2/pkg/auth"
	clib "github.com/gechr/clib/cli/kong"
	"github.com/gechr/clib/help"
	"github.com/gechr/clive"
	"github.com/gechr/clive/updater"
	"github.com/gechr/clive/updater/brew"
	"github.com/gechr/clog"
	"github.com/gechr/clog/fx/spinner"
	"github.com/gechr/clog/level"
	"github.com/gechr/clog/style"
	"github.com/gechr/conductor"
	cli "github.com/gechr/conductor/cli/kong"
	"github.com/gechr/x/terminal"
)

// LevelSuccess is a custom log level for successful completion messages.
const LevelSuccess = clog.Level(3)

const (
	exitCodeUsage  = 2
	exitCodeSignal = 130
)

var (
	// errSilent indicates an error that has already been logged.
	errSilent = errors.New("silent")
	// errInterrupted indicates the operation was cancelled by a signal.
	errInterrupted = errors.New("interrupted")
)

// userError is a user-facing error that should be displayed as-is.
type userError struct {
	exitCode int
	msg      string
}

func (e *userError) Error() string { return e.msg }

// isCanceled reports whether err (or the context) indicates a signal cancellation.
func isCanceled(ctx context.Context, err error) bool {
	return errors.Is(context.Cause(ctx), errInterrupted) || errors.Is(err, context.Canceled)
}

func main() {
	app := conductor.New(conductor.App{
		Name:        "clone",
		DisplayName: "Clone",
		Description: "Clone GitHub repositories in parallel.",
		Module:      "github.com/gechr/clone",
		HelpLong:    "Print long help with examples",
		Updater: brew.New(
			clive.Info{Module: "github.com/gechr/clone"},
			brew.WithName("Clone"),
			brew.WithFormula("clone"),
			brew.WithTap("gechr/tap"),
		),
		ConfigureLog: configureClog,
	})

	root := CLI{}
	prog, err := cli.New(app, &root,
		// Clone builds its own help sections (flag ordering, long-help
		// examples), overriding conductor's default help wiring.
		cli.WithKongOptions(kong.Help(clib.HelpPrinterFunc(
			app.Renderer,
			cloneHelpSections(app.Theme),
			help.WithHelpFlags("Print short help", "Print long help with examples"),
			cloneHelpOrdering(),
			help.WithLongHelp(os.Args, buildExamplesSection()),
		))),
		cli.WithExitCode(exitCode),
	)
	if err != nil {
		clog.Fatal().Err(err).Msg("Failed to build CLI")
	}
	setMethodDefault(prog.Parser)
	os.Exit(prog.Run(os.Args[1:]))
}

// exitCode maps clone's error taxonomy to process exit codes; the Fatal
// branches print and exit directly, preserving the pre-conductor output.
func exitCode(err error) int {
	var ue *userError
	switch {
	case errors.Is(err, errSilent):
		return 1
	case errors.As(err, &ue):
		if ue.msg != "" {
			clog.Fatal().ExitCode(ue.exitCode).Msg(ue.msg)
		}
		return ue.exitCode
	default:
		clog.Fatal().Msg(err.Error())
	}
	return 1
}

// configureClog layers clone's voice (custom success level, symbols, styles,
// spinner) over conductor's defaults; conductor runs it via App.ConfigureLog.
func configureClog() {
	level.Register(LevelSuccess, "success", "OK")

	clog.SetParts(clog.PartSymbol, clog.PartMessage, clog.PartFields)
	clog.SetLevelAlign(clog.AlignNone)
	clog.SetWrap(clog.WrapSoft)

	clog.SetSymbols(clog.LabelMap{
		clog.LevelInfo:  "·",
		LevelSuccess:    "✔︎",
		clog.LevelWarn:  "›",
		clog.LevelError: "✘",
		clog.LevelFatal: "✘",
		clog.LevelDry:   "$",
	})

	green := new(lipgloss.NewStyle().Foreground(lipgloss.Color("2")))
	yellow := new(lipgloss.NewStyle().Foreground(lipgloss.Color("3")))
	clog.SetStyles(&style.Config{
		Message: new(lipgloss.NewStyle().Bold(true)),
		Messages: style.LevelMap{
			clog.LevelInfo:  green,
			LevelSuccess:    green,
			clog.LevelWarn:  yellow,
			clog.LevelError: new(lipgloss.NewStyle().Foreground(lipgloss.Color("1"))),
			clog.LevelFatal: new(lipgloss.NewStyle()),
			clog.LevelDry:   yellow,
		},
		Symbols: style.LevelMap{
			clog.LevelInfo:  new(lipgloss.NewStyle().Foreground(lipgloss.Color("3"))),
			LevelSuccess:    green,
			clog.LevelWarn:  new(lipgloss.NewStyle().Foreground(lipgloss.Color("3"))),
			clog.LevelError: new(lipgloss.NewStyle().Foreground(lipgloss.Color("1"))),
			clog.LevelFatal: new(lipgloss.NewStyle().Foreground(lipgloss.Color("1"))),
			clog.LevelDry:   new(lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)),
		},
	})
	clog.SetSpinnerDefaults(spinner.WithConfig(spinner.DotsBounce))

	// Align clive's self-update glyphs and colours with clone's own symbol set
	// so the updater's lines don't clash with the emoji defaults.
	updater.SetConfig(
		updater.WithUpToDateSymbol("✔︎"),
		updater.WithUpgradedSymbol("↑"),
		updater.WithDowngradedSymbol("↓"),
		updater.WithDoneSymbol("✔︎"),
		updater.WithTrashSymbol("✘"),
		updater.WithUpToDateColor(lipgloss.Color("2")),   // green
		updater.WithUpgradedColor(lipgloss.Color("2")),   // green
		updater.WithDowngradedColor(lipgloss.Color("1")), // red
		updater.WithDoneColor(lipgloss.Color("2")),       // green
	)
}

// Run implements the kong entry point: conductor dispatches here after
// completion preflight, parsing, standard-flag application and the passive
// update check.
func (c *CLI) Run(kctx *kong.Context, app *conductor.Runtime) error {
	if err := c.afterParse(kctx); err != nil {
		return err
	}

	if c.Version {
		app.PrintVersion(false)
		return nil
	}

	dryRunColor = resolveColorEnabled(c.Color)

	envCfg, envErr := loadEnvConfig()
	if envErr != nil {
		return envErr
	}

	// jj is only required when explicitly chosen. For --fetch/--pull we resolve
	// it lazily after per-target VCS detection, and an explicit --git skips jj
	// entirely.
	needJJ := c.VCS == vcsJJ
	binGit, binJJ, depsErr := checkDeps(envCfg, needJJ)
	if depsErr != nil {
		clog.Error().Msg(depsErr.Error())
		return errSilent
	}
	c.binGit = binGit
	c.binJJ = binJJ

	// Best-effort resolve jj for fetch/pull when VCS isn't explicitly set:
	// per-target detection may discover jj-colocated clones that need it.
	// Explicit --git or --jj short-circuits this path.
	if !needJJ && (c.Fetch || c.Pull) && !c.explicitVCS {
		if jj, jjErr := resolveBinPath(envCfg.BinJJ, "CLONE_BIN_JJ", nameJJ); jjErr == nil {
			c.binJJ = jj
		}
	}

	sigCtx, stop := conductor.SignalContext()
	defer stop()
	ctx, cancel := context.WithCancelCause(sigCtx)
	context.AfterFunc(sigCtx, func() {
		cancel(errInterrupted)
	})

	var lister repoLister
	var targets []CloneTarget
	var baseDir string

	targets, baseDir, err := resolveCloneTargets(
		ctx,
		c,
		lazyRepoLister(func() (repoLister, error) {
			if lister != nil {
				return lister, nil
			}
			// With a token, use GraphQL (5,000 req/hour). Without one, fall
			// back to the anonymous REST API: GraphQL has no unauthenticated
			// tier, whereas REST permits anonymous access at 60 req/hour. The
			// reduced limit is surfaced only if it is actually hit, so the
			// tokenless path stays seamless until auth genuinely matters.
			if token := githubToken(); token != "" {
				_ = os.Setenv("GH_TOKEN", token)
				nextLister, listerErr := newGraphQLRepoLister()
				if listerErr != nil {
					return nil, listerErr
				}
				lister = nextLister
			} else {
				lister = newRESTRepoLister()
			}
			return lister, nil
		}),
	)
	if c.Temp && baseDir != "" {
		defer func() { _ = os.Remove(baseDir) }() // remove temp dir if empty
	}
	if err != nil {
		if isCanceled(ctx, err) {
			return &userError{exitCode: exitCodeSignal}
		}
		if !errors.Is(err, errSilent) {
			clog.Error().Msg(err.Error())
		}
		return errSilent
	}

	if err := executeClones(ctx, c, baseDir, targets); err != nil {
		if isCanceled(ctx, err) {
			return &userError{exitCode: exitCodeSignal}
		}
		if !errors.Is(err, errSilent) {
			clog.Error().Msg(err.Error())
		}
		return errSilent
	}

	if c.Print && baseDir != "" {
		fmt.Println(baseDir)
	}
	return nil
}

type lazyRepoLister func() (repoLister, error)

func (f lazyRepoLister) ListOwnerRepos(
	ctx context.Context,
	owner string,
	opts repoListOptions,
) ([]repoInfo, error) {
	lister, err := f()
	if err != nil {
		return nil, err
	}
	return lister.ListOwnerRepos(ctx, owner, opts)
}

func (f lazyRepoLister) ListViewerRepos(
	ctx context.Context,
	source viewerSource,
	opts repoListOptions,
) ([]repoInfo, error) {
	lister, err := f()
	if err != nil {
		return nil, err
	}
	return lister.ListViewerRepos(ctx, source, opts)
}

func (f lazyRepoLister) ResolvePR(
	ctx context.Context,
	owner, repo string,
	number int,
) (prInfo, error) {
	lister, err := f()
	if err != nil {
		return prInfo{}, err
	}
	return lister.ResolvePR(ctx, owner, repo, number)
}

func resolveColorEnabled(mode clog.ColorMode) bool {
	switch mode {
	case clog.ColorAlways:
		return true
	case clog.ColorNever:
		return false
	case clog.ColorAuto:
		return terminal.Is(os.Stderr)
	}
	return false
}

func checkDeps(cfg envConfig, needJJ bool) (string, string, error) {
	binGit, err := resolveBinPath(cfg.BinGit, "CLONE_BIN_GIT", nameGit)
	if err != nil {
		return "", "", err
	}
	var binJJ string
	if needJJ {
		binJJ, err = resolveBinPath(cfg.BinJJ, "CLONE_BIN_JJ", nameJJ)
		if err != nil {
			return "", "", err
		}
	}
	return binGit, binJJ, nil
}

func resolveBinPath(override, envVar, name string) (string, error) {
	if override != "" {
		p, err := exec.LookPath(override)
		if err != nil {
			return "", fmt.Errorf("%s=%q not found: %w", envVar, override, err)
		}
		return p, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("'%[1]s' must be installed (run 'brew install %[1]s')", name)
	}
	return p, nil
}

func githubToken() string {
	cfg, err := loadEnvConfig()
	if err == nil && cfg.GitHubToken != "" {
		return cfg.GitHubToken
	}
	for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if token := os.Getenv(key); token != "" {
			return token
		}
	}
	if token, _ := auth.TokenForHost("github.com"); token != "" {
		return token
	}
	return ""
}
