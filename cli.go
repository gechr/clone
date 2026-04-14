package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/gechr/clib/ansi"
	clib "github.com/gechr/clib/cli/kong"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
)

var version = "dev"

const (
	defaultParallelism = 20
	keywordAll         = "all"
	methodSSH          = schemeSSH
	methodHTTPS        = schemeHTTPS
	nameGit            = "git"
	nameJJ             = "jj"
	vcsGit             = nameGit
	vcsJJ              = nameJJ

	envKeyBinGit      = "CLONE_BIN_GIT"
	envKeyBinJJ       = "CLONE_BIN_JJ"
	envKeyGitHubToken = "CLONE_GITHUB_TOKEN" //nolint:gosec // env var name, not a credential
	envKeyOwner       = "CLONE_OWNER"
	envKeyTmpDir      = "CLONE_TMP_DIR"

	mirrorDocsURL = "https://docs.github.com/en/repositories/creating-and-managing-repositories/duplicating-a-repository"

	prStateOpen = "OPEN"
)

type CLI struct {
	clib.CompletionFlags

	Repos []string `name:"repo" help:"Repositories to clone." arg:"" optional:""`

	Owner string `name:"owner" help:"GitHub owner/organization" short:"O" aliases:"org,organization" placeholder:"<owner>" clib:"terse='Owner/org',group='Filters/1'" env:"CLONE_OWNER"`

	Archived   bool     `name:"archived"   help:"Include archived repositories"   aliases:"archive,archives" clib:"terse='Include archived',group='Filters/2'"`
	Forked     bool     `name:"forked"     help:"Include forked repositories"     aliases:"fork,forks"       clib:"terse='Include forked',group='Filters/2'"`
	Languages  []string `name:"language"   help:"Filter by language (repeatable)"                            clib:"terse='Language',group='Filters/2'"         short:"l" placeholder:"<lang>"`
	Topics     []string `name:"topic"      help:"Filter by topic (repeatable)"                               clib:"terse='Topic',group='Filters/2'"            short:"t" placeholder:"<topic>"`
	Visibility string   `name:"visibility" help:"Filter by visibility"                                       clib:"terse='Visibility',group='Filters/2'"                 placeholder:"<viz>"   default:"all" enum:"all,public,private,internal"`

	IncludePatterns []string `name:"include-pattern" help:"Only clone repositories matching regex (repeatable)" short:"i" placeholder:"<regex>" clib:"hide-long,terse='Include (regex)',group='Filters/3'"`
	Includes        []string `name:"include"         help:"Only clone repositories by exact name (repeatable)"            placeholder:"<name>"  clib:"no-indent,terse='Include',group='Filters/3'"`

	ExcludePatterns []string `name:"exclude-pattern" help:"Skip repositories matching regex (repeatable)" short:"e" placeholder:"<regex>" clib:"hide-long,terse='Exclude (regex)',group='Filters/4'"`
	Excludes        []string `name:"exclude"         help:"Skip repositories by exact name (repeatable)"            placeholder:"<name>"  clib:"no-indent,terse='Exclude',group='Filters/4'"`

	Branch      string `name:"branch"      help:"Clone a specific branch"                                               short:"b" aliases:"bookmark" placeholder:"<name>"   clib:"terse='Branch',group='Options/1'"`
	Depth       int    `name:"depth"       help:"Create a shallow clone of the given depth"                             short:"D"                    placeholder:"<n>"      clib:"terse='Depth',group='Options/1'"                         xor:"shallow"`
	Quick       bool   "name:\"quick\"     help:\"Shallow single-branch clone (alias for `--depth=1 --single-branch`)\" short:\"Q\"                                         clib:\"terse='Quick clone',group='Options/1'\"                 xor:\"shallow\""
	Method      string `name:"method"      help:"Clone method"                                                          short:"m"                    placeholder:"<method>" clib:"terse='Clone method',enum='ssh,https',group='Options/1'"                 default:"ssh" enum:"ssh,https,http" env:"CLONE_METHOD"`
	Mirror      bool   `name:"mirror"      help:"Create a mirror clone"                                                                                                     clib:"terse='Mirror clone',group='Options/1'"                  xor:"fetch"`
	VCS         string `name:"vcs"         help:"Version control system"                                                                              placeholder:"<vcs>"   clib:"terse='VCS',group='Options/1'"                           xor:"vcs"       default:"git" enum:"git,jj"         env:"CLONE_VCS"`
	JJ          bool   "name:\"jj\"        help:\"Clone with `jj` (alias for `--vcs=jj`)\"                                                                                  clib:\"terse='Jujutsu',group='Options/1'\"                                                                                        xor:\"vcs\""
	Git         bool   "name:\"git\"       help:\"Clone with `git` (alias for `--vcs=git`)\"                                                                                clib:\"terse='Git',group='Options/1'\"                                                                                            xor:\"vcs\""
	Directory   string `name:"directory"   help:"Clone into a specific directory"                                        short:"d" aliases:"dir"      placeholder:"<path>"  clib:"terse='Directory',group='Options/2'"                     xor:"location"                                                         type:"path"`
	Temp        bool   `name:"temp"        help:"Clone into a temporary directory"                                       short:"T"                                          clib:"terse='Temporary directory',group='Options/2'"           xor:"location"`
	Print       bool   "name:\"print\"     help:\"Print temp directory path to stdout (requires `--temp`; implies `--quiet`)\"                                              clib:\"terse='Print temp path',group='Options/2'\""
	Fetch       bool   `name:"fetch"       help:"Fetch updates for existing clones instead of skipping"                                                                     clib:"terse='Fetch existing',group='Options/3'"                xor:"fetch"`
	Force       bool   `name:"force"       help:"Overwrite existing clones"                                              short:"f"                                          clib:"terse='Force overwrite',group='Options/3'"               xor:"fetch"`
	Dry         bool   `name:"dry"         help:"Show what would be cloned without cloning"                              short:"n" aliases:"dry-run"                        clib:"terse='Dry run',group='Options/3'"`
	Parallelism int    `name:"parallelism" help:"Number of parallel clones"                                              short:"P"                    placeholder:"<n>"     clib:"terse='Parallelism',group='Options/3'"                                   default:"20"`
	Quiet       bool   `name:"quiet"       help:"Suppress informational output"                                          short:"q"                                          clib:"terse='Quiet',group='Options/4'"                         xor:"verbosity"`
	Verbose     bool   `name:"verbose"     help:"Show additional progress information"                                   short:"v"                                          clib:"terse='Verbose',group='Options/4'"                       xor:"verbosity"`
	Debug       bool   `name:"debug"       help:"Show debug logs"                                                                                                           clib:"terse='Debug logs',group='Options/4'"                    xor:"verbosity"`

	Color   clog.ColorMode `name:"color"   help:"When to use color" clib:"terse='Color mode',complete='values=auto always never',group='Miscellaneous/1'" default:"auto"`
	Version bool           `name:"version" help:"Print version"     clib:"terse='Version',group='Miscellaneous/3'"                                                       short:"V"`

	binGit string `kong:"-"`
	binJJ  string `kong:"-"`

	LanguageFilters [][]string `kong:"-"`
	TopicFilters    [][]string `kong:"-"`
}

func vcsDefault() string {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("CLONE_VCS"))); v != "" {
		return v
	}
	return vcsGit
}

func (c *CLI) Normalize() {
	if c.Method == "http" {
		c.Method = methodHTTPS
	}
	if c.Git {
		c.VCS = vcsGit
	} else if c.JJ {
		c.VCS = vcsJJ
	}
	c.Languages = uniqueFold(c.Languages)
	if c.Quick && c.Depth == 0 {
		c.Depth = 1
	}
	if c.Print {
		c.Quiet = true
	}
}

func (c *CLI) Validate() error {
	if c.Version {
		return nil
	}
	c.Normalize()
	languageGroups, err := parseFilters("language", c.Languages)
	if err != nil {
		return err
	}
	var langs []string
	for _, group := range languageGroups {
		langs = append(langs, group...)
	}
	c.Languages = uniqueFold(langs)
	if len(c.Languages) > 0 {
		c.LanguageFilters = [][]string{c.Languages}
	}
	topicFilters, err := parseFilters("topic", c.Topics)
	if err != nil {
		return err
	}
	c.TopicFilters = uniqueTopicFilters(topicFilters)
	if len(c.Repos) == 0 && len(c.Languages) == 0 && len(c.Topics) == 0 {
		return fmt.Errorf("at least one repository is required")
	}
	if c.Print && !c.Temp {
		return fmt.Errorf("--print requires --temp")
	}
	if c.Parallelism < 1 {
		return fmt.Errorf("--parallelism must be at least 1")
	}
	if c.Depth < 0 {
		return fmt.Errorf("--depth must be non-negative")
	}
	if c.Mirror && c.VCS == vcsJJ {
		return fmt.Errorf("--mirror is not supported with jj (use --vcs=git)")
	}
	return nil
}

func parseFilters(key string, values []string) ([][]string, error) {
	var filters [][]string
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("invalid %s filter %q", key, value)
		}
		for clause := range strings.SplitSeq(value, ",") {
			clause = strings.TrimSpace(clause)
			if clause == "" {
				return nil, fmt.Errorf("invalid %s filter %q", key, value)
			}

			var group []string
			for option := range strings.SplitSeq(clause, "/") {
				option = strings.TrimSpace(option)
				if option == "" {
					return nil, fmt.Errorf("invalid %s filter %q", key, value)
				}
				group = append(group, option)
			}
			filters = append(filters, group)
		}
	}
	return filters, nil
}

func uniqueFold(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniqueTopicFilters(filters [][]string) [][]string {
	seen := make(map[string]struct{}, len(filters))
	out := make([][]string, 0, len(filters))
	for _, group := range filters {
		group = uniqueFold(group)
		keyParts := make([]string, len(group))
		for i, option := range group {
			keyParts[i] = strings.ToLower(strings.TrimSpace(option))
		}
		slices.Sort(keyParts)
		key := strings.Join(keyParts, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, group)
	}
	return out
}

func buildParser(cli *CLI) *kong.Kong {
	th := theme.Default()
	renderer := help.NewRenderer(th)
	return kong.Must(
		cli,
		kong.Name("clone"),
		kong.Description("Clone GitHub repositories in parallel."),
		kong.UsageOnError(),
		kong.Help(clib.HelpPrinterFunc(
			renderer,
			cloneHelpSections(th),
			help.WithHelpFlags("Print short help", "Print long help with examples"),
			cloneHelpOrdering(),
			help.WithFlagDefault("parallelism", strconv.Itoa(defaultParallelism)),
			help.WithLongHelp(os.Args, buildExamplesSection()),
		)),
	)
}

func cloneHelpSections(th *theme.Theme) func(*kong.Context) ([]help.Section, error) {
	return func(ctx *kong.Context) ([]help.Section, error) {
		sections, err := clib.NodeSections(ctx, clib.WithArguments(&CLI{}))
		if err != nil {
			return nil, err
		}
		replaceHelpSectionContent(sections, "Usage", styledUsageText(th))
		replaceHelpSectionContent(sections, "Arguments", styledArgumentsText(th))

		patchHelpFlag(sections, "mirror", func(flag *help.Flag) {
			flag.Desc = fmt.Sprintf(
				"Create a %s clone (git only)",
				ansi.Auto().Hyperlink(mirrorDocsURL, "mirror"),
			)
		})
		patchHelpFlag(sections, "vcs", func(flag *help.Flag) {
			flag.EnumDefault = vcsDefault()
		})

		return sections, nil
	}
}

func cloneHelpOrdering() help.Option {
	return help.OptionFunc(func(sections []help.Section) []help.Section {
		for i := range sections {
			if sections[i].Title != "Miscellaneous" {
				continue
			}

			var (
				otherGroups  []help.Content
				helpGroup    help.Content
				versionGroup help.Content
			)
			for _, content := range sections[i].Content {
				flagGroup, ok := content.(help.FlagGroup)
				if !ok {
					otherGroups = append(otherGroups, content)
					continue
				}

				switch {
				case flagGroupHasLong(flagGroup, "version"):
					versionGroup = content
				case flagGroupHasLong(flagGroup, "help") || flagGroupHasShort(flagGroup, "h"):
					helpGroup = content
				default:
					otherGroups = append(otherGroups, content)
				}
			}

			if versionGroup != nil {
				otherGroups = append(otherGroups, versionGroup)
			}
			if helpGroup != nil {
				otherGroups = append(otherGroups, helpGroup)
			}

			sections[i].Content = otherGroups
			break
		}
		return sections
	})
}

func replaceHelpSectionContent(sections []help.Section, title string, content help.Content) {
	for i := range sections {
		if sections[i].Title != title {
			continue
		}
		sections[i].Content = []help.Content{content}
		return
	}
}

func patchHelpFlag(sections []help.Section, long string, fn func(*help.Flag)) {
	for i := range sections {
		for j, content := range sections[i].Content {
			flagGroup, ok := content.(help.FlagGroup)
			if !ok {
				continue
			}

			changed := false
			for k := range flagGroup {
				if flagGroup[k].Long != long {
					continue
				}
				fn(&flagGroup[k])
				changed = true
			}
			if changed {
				sections[i].Content[j] = flagGroup
				return
			}
		}
	}
}

func flagGroupHasLong(flagGroup help.FlagGroup, long string) bool {
	for _, flag := range flagGroup {
		if flag.Long == long {
			return true
		}
	}
	return false
}

func flagGroupHasShort(flagGroup help.FlagGroup, short string) bool {
	for _, flag := range flagGroup {
		if flag.Short == short {
			return true
		}
	}
	return false
}

func styledUsageText(th *theme.Theme) help.Text {
	return help.Text(fmt.Sprintf(
		"  %s %s %s",
		th.HelpCommand.Render("clone"),
		th.HelpFlag.Render("[options]"),
		th.HelpArg.Render("[<owner>/]<repo>[=<dir>]…"),
	))
}

func styledArgumentsText(th *theme.Theme) help.Text {
	return help.Text(fmt.Sprintf(
		`  %s

    Repositories to clone (use %s to clone everything)

    If specified, %s takes precedence over the %s flag

    Use %s to override the local directory name`,
		th.HelpArg.Render("[<owner>/<repo>[=<dir>]]"),
		th.HelpArg.Render("all"),
		th.HelpArg.Render("<owner>"),
		th.HelpFlag.Render("--owner"),
		th.HelpArg.Render("=<dir>"),
	))
}

func buildExamplesSection() help.Section {
	return help.Section{
		Title: "Examples",
		Content: []help.Content{
			help.Examples{
				{
					Comment: "Clone a specific repository",
					Command: "clone owner/repo",
				},
				{
					Comment: "Clone multiple repositories",
					Command: "clone owner/repo-one owner/repo-two",
				},
				{
					Comment: "Clone all Go repositories",
					Command: "clone --language=Go",
				},
				{
					Comment: "Clone specific repos only if they match filter",
					Command: "clone repo-a repo-b --language=Go",
				},
				{
					Comment: "Quick shallow clone (no history, default branch only)",
					Command: "clone --quick owner/repo",
				},
				{
					Comment: "Clone a repository from a different owner",
					Command: "clone other-owner/repo",
				},
				{
					Comment: "Clone into a custom directory name",
					Command: "clone owner/repo=local-dir",
				},
				{
					Comment: "Clone all repositories from a different owner",
					Command: "clone -O other-owner all",
				},
				{
					Comment: "Clone into a specific directory",
					Command: "clone -d ~/projects/go --language=Go",
				},
				{
					Comment: "Clone a specific branch with shallow depth",
					Command: "clone --branch=main --depth=1 owner/repo",
				},
				{
					Comment: "Clone using HTTPS instead of SSH",
					Command: "clone --method=https owner/repo",
				},
				{
					Comment: "Clone from a GitHub URL",
					Command: "clone https://github.com/owner/repo",
				},
				{
					Comment: "Clone a pull request (checks out the PR branch)",
					Command: "clone https://github.com/owner/repo/pull/21",
				},
				{
					Comment: "Clone a PR using shorthand",
					Command: "clone owner/repo#21",
				},
				{
					Comment: "Clone from an SSH clone URL",
					Command: "clone git@github.com:owner/repo.git",
				},
				{
					Comment: "Fetch existing repos, clone new ones",
					Command: "clone --fetch --language=Go",
				},
			},
		},
	}
}
