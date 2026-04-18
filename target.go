package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gechr/clog"
	"github.com/gechr/clog/fx"
)

type repoRequest struct {
	Dir           string
	ExplicitOwner bool
	Host          string
	Name          string
	Owner         string
	PullRequest   string
	Source        string
}

type CloneTarget struct {
	BinGit        string
	BinJJ         string
	Branch        string
	CustomDest    bool
	Depth         int
	Dest          string
	ExplicitOwner bool
	Label         string
	Mirror        bool
	Owner         string
	PRHeadRef     string
	PullRequest   string
	Repo          string
	RepoURL       string
	SingleBranch  bool
	Slug          string
	Source        string
	VCS           string
}

// expandMultiPR rewrites `<repo>#N,M,P-Q` tokens into separate entries per
// PR number. Single-PR references are passed through unchanged; same-repo
// dest collisions across entries get resolved later in resolveCloneTargets.
func expandMultiPR(repos []string) ([]string, error) {
	out := make([]string, 0, len(repos))
	for _, arg := range repos {
		repoText, dir, hasDir := strings.Cut(arg, "=")
		before, prPart, hasPR := strings.Cut(repoText, "#")
		if !hasPR || !isMultiPR(prPart) {
			out = append(out, arg)
			continue
		}
		if hasDir {
			return nil, fmt.Errorf("%q: =%s cannot be combined with multi-PR reference", arg, dir)
		}
		nums, err := parsePRList(prPart)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", arg, err)
		}
		for _, num := range nums {
			out = append(out, fmt.Sprintf("%s#%d", before, num))
		}
	}
	return out, nil
}

// isMultiPR returns true when prPart uses `,` or `-` as a list/range
// separator between numbers. A leading `-` (e.g. `#-1`) is *not* multi-PR —
// it falls through to normal parsing which rejects negative numbers.
func isMultiPR(prPart string) bool {
	if prPart == "" || !isDigit(rune(prPart[0])) {
		return false
	}
	return strings.ContainsAny(prPart, ",-")
}

// parsePRList parses a PR list like "1,2,5-7" into []int{1, 2, 5, 6, 7}.
// Ranges are inclusive on both ends; negative or zero numbers are rejected.
func parsePRList(spec string) ([]int, error) {
	var nums []int
	seen := make(map[int]struct{})
	for segment := range strings.SplitSeq(spec, ",") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil, fmt.Errorf("empty PR number in %q", spec)
		}
		lo, hi, ok := parsePRRange(segment)
		if !ok {
			return nil, fmt.Errorf("invalid PR reference %q", segment)
		}
		for n := lo; n <= hi; n++ {
			if _, dup := seen[n]; dup {
				continue
			}
			seen[n] = struct{}{}
			nums = append(nums, n)
		}
	}
	return nums, nil
}

func parsePRRange(segment string) (int, int, bool) {
	if before, after, hasDash := strings.Cut(segment, "-"); hasDash {
		lo, errLo := strconv.Atoi(before)
		hi, errHi := strconv.Atoi(after)
		if errLo != nil || errHi != nil || lo <= 0 || hi < lo {
			return 0, 0, false
		}
		return lo, hi, true
	}
	n, err := strconv.Atoi(segment)
	if err != nil || n <= 0 {
		return 0, 0, false
	}
	return n, n, true
}

func parseRepoRequest(input, defaultOwner string) (repoRequest, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return repoRequest{}, fmt.Errorf("repository argument cannot be empty")
	}

	repoText := raw
	dir := ""
	if left, right, ok := strings.Cut(raw, "="); ok {
		if right == "" {
			return repoRequest{}, fmt.Errorf("missing directory in %q", raw)
		}
		repoText = left
		dir = right
	}

	if req, ok := parseRepoURL(repoText); ok {
		req.Dir = dir
		return req, nil
	}

	owner := defaultOwner
	name := repoText
	explicitOwner := false
	if before, after, ok := strings.Cut(repoText, "/"); ok {
		if before == "" || after == "" || strings.Contains(after, "/") {
			return repoRequest{}, fmt.Errorf("invalid repository %q", raw)
		}
		owner = before
		name = after
		explicitOwner = true
	}

	var pr string
	if namePart, prPart, ok := strings.Cut(name, "#"); ok {
		name = namePart
		pr = prPart
	}

	if name != keywordAll && !isValidRepoName(name) {
		return repoRequest{}, fmt.Errorf("invalid repository %q", raw)
	}
	if name == keywordAll && dir != "" {
		return repoRequest{}, fmt.Errorf("%q cannot be combined with =<dir>", raw)
	}
	if name == keywordAll && pr != "" {
		return repoRequest{}, fmt.Errorf("%q cannot be combined with PR references", keywordAll)
	}

	return repoRequest{
		ExplicitOwner: explicitOwner,
		Owner:         owner,
		Name:          strings.TrimSuffix(name, dotGit),
		Dir:           dir,
		PullRequest:   pr,
	}, nil
}

func parseRepoURL(repoText string) (repoRequest, bool) {
	return parseForgeURL(repoText)
}

const (
	minPullSegments  = 4   // owner/repo/pull/N
	maxRepoNameBytes = 255 // common filesystem NAME_MAX for a single path component
)

func ensureDefaultOwner(defaultOwner string, nonGitHub bool) (string, error) {
	if defaultOwner != "" {
		return defaultOwner, nil
	}
	if nonGitHub {
		return "", fmt.Errorf("owner must be specified explicitly for non-GitHub forges")
	}
	return resolveDefaultOwner()
}

func resolveViewerTargets(
	ctx context.Context,
	cli *CLI,
	lister repoLister,
) ([]CloneTarget, string, error) {
	if len(cli.Repos) > 0 {
		return nil, "", fmt.Errorf(
			"--starred/--watching cannot be combined with explicit repositories",
		)
	}

	var sources []viewerSource
	if cli.Starred {
		sources = append(sources, viewerStarred)
	}
	if cli.Watching {
		sources = append(sources, viewerWatching)
	}

	envCfg, cfgErr := loadEnvConfig()
	if cfgErr != nil {
		return nil, "", cfgErr
	}

	baseDir, err := resolveBaseDirectory(cli, envCfg.TempDir)
	if err != nil {
		return nil, "", err
	}

	if cli.forge.Host == "" {
		cli.forge = forgeRegistry[forgeGitHub]
	}

	var viewerRepos []repoInfo
	s := viewerSpinner(cli)
	listErr := s.Wait(ctx, func(_ context.Context) error {
		seen := make(map[string]struct{})
		for _, source := range sources {
			fetched, fetchErr := lister.ListViewerRepos(source, repoListOptions{
				IncludeArchived: cli.Archived,
				IncludeForked:   cli.Forked,
				Languages:       cli.Languages,
				Stars:           cli.StarsFilter,
				TopicFilters:    cli.TopicFilters,
				Visibility:      cli.Visibility,
			})
			if fetchErr != nil {
				return fetchErr
			}
			for _, repo := range fetched {
				key := repo.Owner + "/" + repo.Name
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				viewerRepos = append(viewerRepos, repo)
			}
		}
		if len(viewerRepos) == 0 {
			return &userError{msg: "No repositories matched"}
		}
		return nil
	}).
		OnSuccessLevel(LevelSuccess).
		Msg("Fetched")
	if listErr != nil {
		return nil, baseDir, errSilent
	}

	requests := make([]repoRequest, 0, len(viewerRepos))
	for _, repo := range viewerRepos {
		requests = append(requests, repoRequest{
			ExplicitOwner: true,
			Host:          cli.forge.Host,
			Owner:         repo.Owner,
			Name:          repo.Name,
		})
	}

	requests, err = applyNameFilters(requests, cli)
	if err != nil {
		return nil, baseDir, err
	}
	requests = dedupeRequests(requests)
	if len(requests) == 0 {
		return nil, baseDir, &userError{msg: "Filter returned no repositories"}
	}

	targets, err := buildTargetsFromRequests(cli, baseDir, requests)
	if err != nil {
		return nil, baseDir, err
	}
	return targets, baseDir, nil
}

// addPRSuffix sets Dir=<name>#<N> on any PR request without an explicit
// directory, so PR clones land alongside plain clones without colliding.
func addPRSuffix(requests []repoRequest) {
	for i, req := range requests {
		if req.PullRequest == "" || req.Dir != "" {
			continue
		}
		requests[i].Dir = req.Name + "#" + req.PullRequest
	}
}

// buildTargetsFromRequests converts fully-resolved repoRequests into CloneTargets.
// Used by the viewer path, which has no PR references to resolve.
func buildTargetsFromRequests(
	cli *CLI,
	baseDir string,
	requests []repoRequest,
) ([]CloneTarget, error) {
	targets := make([]CloneTarget, 0, len(requests))
	for _, req := range requests {
		destName := req.Dir
		if destName == "" {
			if cli.Mirror {
				destName = req.Name + dotGit
			} else {
				destName = req.Name
			}
		}
		dest := destName
		if baseDir != "" {
			dest = filepath.Join(baseDir, destName)
		}
		slug := req.Owner + "/" + req.Name
		if req.Host == "" {
			req.Host = cli.forge.Host
		}
		targets = append(targets, CloneTarget{
			BinGit:        cli.binGit,
			BinJJ:         cli.binJJ,
			Branch:        cli.Branch,
			CustomDest:    req.Dir != "",
			Depth:         cli.Depth,
			Dest:          dest,
			ExplicitOwner: req.ExplicitOwner,
			Label:         slug,
			Mirror:        cli.Mirror,
			Owner:         req.Owner,
			Repo:          req.Name,
			RepoURL:       repoWebURL(req.Host, slug),
			SingleBranch:  cli.Quick,
			Slug:          slug,
			Source:        resolveCloneSource(cli.Method, req, cli.forge),
			VCS:           cli.VCS,
		})
	}
	if err := detectDestinationClashes(targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func viewerSpinner(cli *CLI) *fx.Builder {
	s := clog.Spinner("Fetching")
	if f := formatTopicFilters(cli.LanguageFilters); f != "" {
		s = s.Str(pluralize("language", cli.LanguageFilters), f)
	}
	if f := formatTopicFilters(cli.TopicFilters); f != "" {
		s = s.Str(pluralize("topic", cli.TopicFilters), f)
	}
	if f := formatRangeFilter(cli.StarsFilter); f != "" {
		s = s.Str(keyStars, f)
	}
	if cli.Starred {
		s = s.Bool("starred", true)
	}
	if cli.Watching {
		s = s.Bool("watching", true)
	}
	return s
}

func resolveCloneTargets(
	ctx context.Context,
	cli *CLI,
	lister repoLister,
) ([]CloneTarget, string, error) {
	if cli.Starred || cli.Watching {
		return resolveViewerTargets(ctx, cli, lister)
	}

	repos, err := expandMultiPR(cli.Repos)
	if err != nil {
		return nil, "", err
	}
	if len(repos) == 0 &&
		(len(cli.Languages) > 0 || len(cli.Topics) > 0 || cli.StarsFilter.present() || cli.Owner != "") {
		repos = []string{keywordAll}
	}

	envCfg, cfgErr := loadEnvConfig()
	if cfgErr != nil {
		return nil, "", cfgErr
	}

	if cli.forge.Host == "" {
		cli.forge = forgeRegistry[forgeGitHub]
	}
	nonGitHub := cli.forge.Host != hostGitHub

	defaultOwner := resolveOwnerAlias(strings.TrimSpace(cli.Owner), envCfg.Aliases)
	if strings.EqualFold(defaultOwner, atMe) {
		if nonGitHub {
			return nil, "", fmt.Errorf("@me is only currently supported for GitHub hosts")
		}
		var resolved string
		resolved, err = ghOwnerLookup()
		if err != nil {
			return nil, "", err
		}
		defaultOwner = resolved
	}

	requests := make([]repoRequest, 0, len(repos))
	for _, arg := range repos {
		var req repoRequest
		req, err = parseRepoRequest(arg, defaultOwner)
		if err != nil {
			return nil, "", err
		}
		req.Owner = resolveOwnerAlias(req.Owner, envCfg.Aliases)
		if strings.EqualFold(req.Owner, atMe) {
			if nonGitHub {
				return nil, "", fmt.Errorf(
					"@me is only currently supported for GitHub hosts",
				)
			}
			req.Owner, err = ghOwnerLookup()
			if err != nil {
				return nil, "", err
			}
		}
		if req.Owner == "" {
			defaultOwner, err = ensureDefaultOwner(defaultOwner, nonGitHub)
			if err != nil {
				return nil, "", err
			}
			req.Owner = defaultOwner
		}
		if req.Name == keywordAll && nonGitHub {
			return nil, "", fmt.Errorf(
				"%q is only currently supported for GitHub hosts",
				keywordAll,
			)
		}
		if req.PullRequest != "" && nonGitHub {
			return nil, "", fmt.Errorf(
				"PR references are only currently supported for GitHub hosts",
			)
		}
		requests = append(requests, req)
	}

	hasPR := false
	for _, req := range requests {
		if req.PullRequest != "" {
			hasPR = true
			break
		}
	}
	if hasPR && cli.Branch != "" {
		return nil, "", fmt.Errorf("--branch and PR references are mutually exclusive")
	}
	if hasPR && cli.Mirror {
		return nil, "", fmt.Errorf("--mirror is not supported with PR references")
	}

	baseDir, err := resolveBaseDirectory(cli, envCfg.TempDir)
	if err != nil {
		return nil, "", err
	}

	needQuery := requiresRepoQuery(cli, requests)
	repoIndex := make(map[string]map[string]repoInfo)
	if needQuery {
		for _, owner := range requestedOwners(requests) {
			var ownerRepos []repoInfo
			s := fetchSpinner(owner, cli)
			listErr := s.Wait(ctx, func(_ context.Context) error {
				var listErr error
				ownerRepos, listErr = lister.ListOwnerRepos(owner, repoListOptions{
					IncludeArchived: cli.Archived,
					IncludeForked:   cli.Forked,
					Languages:       cli.Languages,
					Stars:           cli.StarsFilter,
					TopicFilters:    cli.TopicFilters,
					Visibility:      cli.Visibility,
				})
				if listErr != nil {
					return listErr
				}
				if len(ownerRepos) == 0 {
					return &userError{msg: "No repositories matched"}
				}
				return nil
			}).
				OnSuccessLevel(LevelSuccess).
				Msg("Fetched")
			if listErr != nil {
				return nil, baseDir, errSilent
			}
			index := make(map[string]repoInfo, len(ownerRepos))
			for _, repo := range ownerRepos {
				index[repo.Name] = repo
			}
			repoIndex[owner] = index
		}
	}

	selected := make([]repoRequest, 0, len(requests))
	for _, req := range requests {
		if req.Name == keywordAll {
			names := make([]string, 0, len(repoIndex[req.Owner]))
			for name := range repoIndex[req.Owner] {
				names = append(names, name)
			}
			slices.SortFunc(names, func(a, b string) int {
				return strings.Compare(strings.ToLower(a), strings.ToLower(b))
			})
			for _, name := range names {
				selected = append(selected, repoRequest{Owner: req.Owner, Name: name})
			}
			continue
		}

		if needQuery {
			if _, ok := repoIndex[req.Owner][req.Name]; !ok {
				continue
			}
		}
		selected = append(selected, req)
	}

	selected, err = applyNameFilters(selected, cli)
	if err != nil {
		return nil, baseDir, err
	}

	selected = dedupeRequests(selected)
	if len(selected) == 0 {
		return nil, baseDir, &userError{msg: "Filter returned no repositories"}
	}

	addPRSuffix(selected)

	type prResolution struct {
		headRef   string
		useBranch bool
	}
	prMap := make(map[string]prResolution)
	var prCount int
	for _, req := range selected {
		if req.PullRequest != "" {
			prCount++
		}
	}
	kept := selected[:0]
	for _, req := range selected {
		if req.PullRequest == "" {
			kept = append(kept, req)
			continue
		}
		number, numErr := strconv.Atoi(req.PullRequest)
		if numErr != nil || number <= 0 {
			return nil, baseDir, fmt.Errorf(
				"invalid PR number %q for %s/%s",
				req.PullRequest,
				req.Owner,
				req.Name,
			)
		}
		info, resolveErr := lister.ResolvePR(req.Owner, req.Name, number)
		if resolveErr != nil {
			reason := resolveErr.Error()
			if inner := errors.Unwrap(resolveErr); inner != nil {
				reason = inner.Error()
			}
			clog.Warn().
				Str("repository", req.Owner+pathSep+req.Name).
				Str("pr", req.PullRequest).
				Str("reason", reason).
				Msg("Skipping")
			continue
		}
		key := prKey(req)
		useBranch := len(selected) == 1 && prCount == 1 && !info.IsCrossRepository &&
			info.State == prStateOpen
		prMap[key] = prResolution{headRef: info.HeadRefName, useBranch: useBranch}
		kept = append(kept, req)
	}
	selected = kept
	if len(selected) == 0 {
		return nil, baseDir, &userError{msg: "No pull requests resolved"}
	}

	targets := make([]CloneTarget, 0, len(selected))
	for _, req := range selected {
		destName := req.Dir
		if destName == "" {
			if cli.Mirror {
				destName = req.Name + dotGit
			} else {
				destName = req.Name
			}
		}

		dest := destName
		if baseDir != "" {
			dest = filepath.Join(baseDir, destName)
		}

		slug := req.Owner + "/" + req.Name

		if req.Host == "" {
			req.Host = cli.forge.Host
		}

		target := CloneTarget{
			BinGit:        cli.binGit,
			BinJJ:         cli.binJJ,
			Branch:        cli.Branch,
			CustomDest:    req.Dir != "",
			Depth:         cli.Depth,
			Dest:          dest,
			ExplicitOwner: req.ExplicitOwner,
			Mirror:        cli.Mirror,
			Owner:         req.Owner,
			Repo:          req.Name,
			RepoURL:       repoWebURL(req.Host, slug),
			SingleBranch:  cli.Quick,
			Slug:          slug,
			Source:        resolveCloneSource(cli.Method, req, cli.forge),
			VCS:           cli.VCS,
		}

		if req.PullRequest != "" {
			res := prMap[prKey(req)]
			if res.useBranch {
				target.Branch = res.headRef
			} else {
				target.PullRequest = req.PullRequest
				target.PRHeadRef = res.headRef
			}
		}

		targets = append(targets, target)
	}

	if err := detectDestinationClashes(targets); err != nil {
		return nil, baseDir, err
	}

	for i := range targets {
		if targets[i].ExplicitOwner {
			targets[i].Label = targets[i].Slug
		} else {
			targets[i].Label = targets[i].Repo
		}
	}

	return targets, baseDir, nil
}

func resolveBaseDirectory(cli *CLI, tempDir string) (string, error) {
	switch {
	case cli.Temp:
		return os.MkdirTemp(tempDir, "clone-*")
	case cli.Directory != "":
		return filepath.Clean(cli.Directory), nil
	default:
		return "", nil
	}
}

func requiresRepoQuery(cli *CLI, requests []repoRequest) bool {
	if len(cli.Languages) > 0 || len(cli.Topics) > 0 || cli.Visibility != keywordAll ||
		cli.Archived ||
		cli.Forked ||
		cli.StarsFilter.present() {
		return true
	}
	for _, req := range requests {
		if req.Name == keywordAll {
			return true
		}
	}
	return false
}

func requestedOwners(requests []repoRequest) []string {
	seen := map[string]struct{}{}
	owners := make([]string, 0, len(requests))
	for _, req := range requests {
		if _, ok := seen[req.Owner]; ok {
			continue
		}
		seen[req.Owner] = struct{}{}
		owners = append(owners, req.Owner)
	}
	slices.Sort(owners)
	return owners
}

func applyNameFilters(requests []repoRequest, cli *CLI) ([]repoRequest, error) {
	includePatterns, err := compileRegexps(cli.IncludePatterns)
	if err != nil {
		return nil, err
	}
	excludePatterns, err := compileRegexps(cli.ExcludePatterns)
	if err != nil {
		return nil, err
	}

	filtered := make([]repoRequest, 0, len(requests))
	for _, req := range requests {
		if matchesExact(req.Name, cli.Excludes) || matchesAnyRegexp(excludePatterns, req.Name) {
			continue
		}

		if len(cli.Includes) > 0 || len(includePatterns) > 0 {
			if !matchesExact(req.Name, cli.Includes) &&
				!matchesAnyRegexp(includePatterns, req.Name) {
				continue
			}
		}

		filtered = append(filtered, req)
	}
	return filtered, nil
}

func compileRegexps(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

func matchesExact(value string, exact []string) bool {
	for _, item := range exact {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

func matchesAnyRegexp(patterns []*regexp.Regexp, value string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func formatTopicFilters(filters [][]string) string {
	parts := make([]string, 0, len(filters))
	for _, group := range filters {
		part := formatOR(group)
		if len(group) > 1 && len(filters) > 1 {
			part = "(" + part + ")"
		}
		parts = append(parts, part)
	}
	return formatAND(parts)
}

func fetchSpinner(owner string, cli *CLI) *fx.Builder {
	s := clog.Spinner("Fetching").
		Link(keyOwner, "https://github.com/"+owner, owner)
	if f := formatTopicFilters(cli.LanguageFilters); f != "" {
		s = s.Str(pluralize("language", cli.LanguageFilters), f)
	}
	if f := formatTopicFilters(cli.TopicFilters); f != "" {
		s = s.Str(pluralize("topic", cli.TopicFilters), f)
	}
	if f := formatRangeFilter(cli.StarsFilter); f != "" {
		s = s.Str(keyStars, f)
	}
	return s
}

// formatRangeFilter renders a rangeFilter as a concise, human-readable string.
func formatRangeFilter(r rangeFilter) string {
	switch {
	case !r.present():
		return ""
	case r.min > 0 && r.max > 0 && r.min == r.max:
		return fmt.Sprintf("%d", r.min)
	case r.min > 0 && r.max > 0:
		return fmt.Sprintf("%d..%d", r.min, r.max)
	case r.min > 0:
		return fmt.Sprintf(">=%d", r.min)
	default:
		return fmt.Sprintf("<=%d", r.max)
	}
}

func pluralize(singular string, filters [][]string) string {
	n := 0
	for _, group := range filters {
		n += len(group)
	}
	if n > 1 {
		return singular + "s"
	}
	return singular
}

func formatAND(values []string) string {
	return strings.Join(values, " AND ")
}

func formatOR(values []string) string {
	return strings.Join(values, " OR ")
}

func dedupeRequests(requests []repoRequest) []repoRequest {
	seen := map[string]struct{}{}
	out := make([]repoRequest, 0, len(requests))
	for _, req := range requests {
		key := req.Owner + "/" + req.Name + "#" + req.PullRequest + "=" + req.Dir
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, req)
	}
	return out
}

func detectDestinationClashes(targets []CloneTarget) error {
	type entry struct {
		slug    string
		repoURL string
	}
	order := make([]string, 0)
	groups := make(map[string][]entry)
	for _, target := range targets {
		if _, ok := groups[target.Dest]; !ok {
			order = append(order, target.Dest)
		}
		groups[target.Dest] = append(groups[target.Dest], entry{
			slug:    target.Slug,
			repoURL: target.RepoURL,
		})
	}
	var clashes int
	for _, dest := range order {
		entries := groups[dest]
		if len(entries) < 2 { //nolint:mnd // a clash requires at least 2
			continue
		}
		links := make([]clog.Link, len(entries))
		for i, e := range entries {
			links[i] = clog.Link{URL: e.repoURL, Text: e.slug}
		}
		clog.Error().
			Links("repositories", links).
			Path("destination", dest).
			Msg("Destination clash")
		clashes++
	}
	if clashes > 0 {
		return errSilent
	}
	return nil
}

func prKey(req repoRequest) string {
	return req.Owner + "/" + req.Name + "#" + req.PullRequest
}

func resolveCloneSource(method string, req repoRequest, forge forgeConfig) string {
	if req.Source != "" {
		return req.Source
	}
	scheme := schemeSSH
	if method == methodHTTPS {
		scheme = schemeHTTPS
	}
	return forgeSource(scheme, forge.Host, req.Owner+pathSep+req.Name, forge.GitSuffix)
}

func isValidRepoName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if len(name) > maxRepoNameBytes {
		return false
	}
	for _, r := range name {
		switch {
		case isLower(r), isUpper(r), isDigit(r):
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
