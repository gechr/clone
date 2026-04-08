package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/alecthomas/kong"
	"github.com/cli/go-gh/v2/pkg/auth"
	clib "github.com/gechr/clib/cli/kong"
	"github.com/gechr/clib/complete"
	"github.com/gechr/clog"
	"github.com/gechr/clog/fx/spinner"
	"github.com/gechr/clog/level"
	"github.com/gechr/clog/style"
)

// LevelSuccess is a custom log level for successful completion messages.
const LevelSuccess = clog.Level(1)

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
	msg      string
	exitCode int
}

func (e *userError) Error() string { return e.msg }

func main() {
	configureClog()

	err := run()
	if err == nil {
		return
	}

	var ue *userError
	switch {
	case errors.Is(err, errSilent):
		os.Exit(1)
	case errors.As(err, &ue):
		if ue.msg != "" {
			clog.Fatal().ExitCode(ue.exitCode).Msg(ue.msg)
		}
		os.Exit(ue.exitCode)
	default:
		clog.Fatal().Msg(err.Error())
	}
}

func configureClog() {
	level.Register(LevelSuccess, "success", "OK")
	clog.SetOutput(clog.Stderr(clog.ColorAuto))

	clog.SetParts(clog.PartSymbol, clog.PartMessage, clog.PartFields)
	clog.SetLevelAlign(clog.AlignNone)
	clog.SetSliceSeparator(" ")

	clog.SetSymbols(clog.LabelMap{
		clog.LevelInfo:  "·",
		LevelSuccess:    "✔︎",
		clog.LevelWarn:  "›",
		clog.LevelError: "✘",
		clog.LevelFatal: "✘",
	})

	green := new(lipgloss.NewStyle().Foreground(lipgloss.Color("2")))
	clog.SetStyles(&style.Config{
		Message: new(lipgloss.NewStyle().Bold(true)),
		Messages: style.LevelMap{
			clog.LevelInfo:  green,
			LevelSuccess:    green,
			clog.LevelWarn:  new(lipgloss.NewStyle().Foreground(lipgloss.Color("3"))),
			clog.LevelError: new(lipgloss.NewStyle().Foreground(lipgloss.Color("1"))),
			clog.LevelFatal: new(lipgloss.NewStyle()),
		},
		Symbols: style.LevelMap{
			clog.LevelInfo:  new(lipgloss.NewStyle().Foreground(lipgloss.Color("3"))),
			LevelSuccess:    green,
			clog.LevelWarn:  new(lipgloss.NewStyle().Foreground(lipgloss.Color("3"))),
			clog.LevelError: new(lipgloss.NewStyle().Foreground(lipgloss.Color("1"))),
			clog.LevelFatal: new(lipgloss.NewStyle().Foreground(lipgloss.Color("1"))),
		},
	})
	clog.SetSpinnerStyle(spinner.DotsBounce)
}

func run() error {
	cli := CLI{}
	parser := buildParser(&cli)

	_, parseErr := parser.Parse(os.Args[1:])

	flags, flagsErr := clib.Reflect(&cli)
	if flagsErr != nil {
		return flagsErr
	}
	gen := complete.NewGenerator("clone").FromFlags(flags)
	gen.Specs = append(gen.Specs,
		complete.Spec{ShortFlag: "h", Terse: "Print short help"},
		complete.Spec{LongFlag: "help", Terse: "Print long help with examples"},
	)
	handled, err := cli.Handle(gen, nil)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	if parseErr != nil {
		var parseKongErr *kong.ParseError
		if errors.As(parseErr, &parseKongErr) {
			return &userError{msg: parseErr.Error(), exitCode: parseKongErr.ExitCode()}
		}
		return &userError{msg: parseErr.Error(), exitCode: exitCodeUsage}
	}

	if cli.Version {
		fmt.Println(version)
		return nil
	}

	clog.SetColorMode(cli.Color)
	clog.SetVerbose(cli.Debug)

	binGit, binJJ, depsErr := checkDeps(cli.VCS)
	if depsErr != nil {
		clog.Error().Msg(depsErr.Error())
		return errSilent
	}
	cli.binGit = binGit
	cli.binJJ = binJJ

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancelCause(sigCtx)
	context.AfterFunc(sigCtx, func() {
		cancel(errInterrupted)
	})

	var lister repoLister
	var targets []CloneTarget
	var baseDir string

	targets, baseDir, err = resolveCloneTargets(
		ctx,
		&cli,
		lazyRepoLister(func() (repoLister, error) {
			if lister != nil {
				return lister, nil
			}
			if authErr := ensureGHAuth(); authErr != nil {
				return nil, authErr
			}
			nextLister, listerErr := newGraphQLRepoLister()
			if listerErr != nil {
				return nil, listerErr
			}
			lister = nextLister
			return lister, nil
		}),
	)
	if cli.Temp && baseDir != "" {
		defer func() { _ = os.Remove(baseDir) }() // remove temp dir if empty
	}
	if err != nil {
		if errors.Is(context.Cause(ctx), errInterrupted) {
			return &userError{exitCode: exitCodeSignal}
		}
		if !errors.Is(err, errSilent) {
			clog.Error().Msg(err.Error())
		}
		return errSilent
	}

	if err := executeClones(ctx, &cli, baseDir, targets); err != nil {
		if errors.Is(context.Cause(ctx), errInterrupted) {
			return &userError{exitCode: exitCodeSignal}
		}
		return errSilent
	}

	if cli.Print && baseDir != "" {
		fmt.Println(baseDir)
	}
	return nil
}

type lazyRepoLister func() (repoLister, error)

func (f lazyRepoLister) ListOwnerRepos(owner string, opts repoListOptions) ([]repoInfo, error) {
	lister, err := f()
	if err != nil {
		return nil, err
	}
	return lister.ListOwnerRepos(owner, opts)
}

func (f lazyRepoLister) ResolvePR(owner, repo string, number int) (prInfo, error) {
	lister, err := f()
	if err != nil {
		return prInfo{}, err
	}
	return lister.ResolvePR(owner, repo, number)
}

func checkDeps(vcs string) (string, string, error) {
	binGit, err := resolveBinPath(envKeyBinGit, nameGit)
	if err != nil {
		return "", "", err
	}
	var binJJ string
	if vcs == vcsJJ {
		binJJ, err = resolveBinPath(envKeyBinJJ, nameJJ)
		if err != nil {
			return "", "", err
		}
	}
	return binGit, binJJ, nil
}

func resolveBinPath(envKey, name string) (string, error) {
	if v := os.Getenv(envKey); v != "" {
		p, err := exec.LookPath(v)
		if err != nil {
			return "", fmt.Errorf("%s=%q not found: %w", envKey, v, err)
		}
		return p, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("'%[1]s' must be installed (run 'brew install %[1]s')", name)
	}
	return p, nil
}

var tokenEnvKeys = []string{envKeyGitHubToken, "GITHUB_TOKEN", "GH_TOKEN"}

func githubToken() string {
	for _, key := range tokenEnvKeys {
		if token := os.Getenv(key); token != "" {
			return token
		}
	}
	if token, _ := auth.TokenForHost("github.com"); token != "" {
		return token
	}
	return ""
}

func ensureGHAuth() error {
	token := githubToken()
	if token == "" {
		return fmt.Errorf(
			"not authenticated with GitHub (set %s or run 'gh auth login')",
			tokenEnvKeys[0],
		)
	}
	_ = os.Setenv("GH_TOKEN", token)
	return nil
}
