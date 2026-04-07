package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gechr/clog"
)

type repoRequest struct {
	Dir           string
	ExplicitOwner bool
	Name          string
	Owner         string
	Source        string
	PullRequest   string
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

	if owner, name, source, pr, ok := parseRepoURL(repoText); ok {
		return repoRequest{
			ExplicitOwner: true,
			Owner:         owner,
			Name:          name,
			Dir:           dir,
			Source:        source,
			PullRequest:   pr,
		}, nil
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

	if name == "" {
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
		Name:          strings.TrimSuffix(name, ".git"),
		Dir:           dir,
		PullRequest:   pr,
	}, nil
}

func parseRepoURL(repoText string) (string, string, string, string, bool) {
	switch {
	case strings.HasPrefix(repoText, "git@github.com:"):
		return parseGitHubPath(
			strings.TrimPrefix(repoText, "git@github.com:"),
			"git@github.com:%s.git",
		)
	case strings.HasPrefix(repoText, "https://") || strings.HasPrefix(repoText, "http://"):
		parsed, err := url.Parse(repoText)
		if err != nil {
			return "", "", "", "", false
		}
		host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
		if host != "github.com" {
			return "", "", "", "", false
		}
		path := strings.TrimPrefix(parsed.Path, "/")
		if parsed.Fragment != "" {
			path += "#" + parsed.Fragment
		}
		return parseGitHubPath(
			path,
			fmt.Sprintf("%s://%s/%%s.git", parsed.Scheme, parsed.Host),
		)
	case strings.HasPrefix(repoText, "github.com/"):
		return parseGitHubPath(
			strings.TrimPrefix(repoText, "github.com/"),
			"https://github.com/%s.git",
		)
	default:
		return "", "", "", "", false
	}
}

const minPullSegments = 4 // owner/repo/pull/N

func parseGitHubPath(raw, sourceFmt string) (string, string, string, string, bool) {
	clean := strings.TrimSuffix(raw, "/")

	var owner, name, pr string
	switch {
	case strings.Contains(clean, "/pull/"):
		segments := strings.Split(clean, "/")
		if len(segments) >= minPullSegments {
			owner = segments[0]
			name = segments[1]
			pr = segments[3]
		}
	case strings.Contains(clean, "#"):
		path, fragment, _ := strings.Cut(clean, "#")
		pr = fragment
		owner, name, _ = strings.Cut(path, "/")
	default:
		owner, name, _ = strings.Cut(clean, "/")
	}

	if owner == "" || name == "" {
		return "", "", "", "", false
	}

	trimmedName := strings.TrimSuffix(name, ".git")
	var source string
	if sourceFmt != "" {
		source = fmt.Sprintf(sourceFmt, owner+"/"+trimmedName)
	} else {
		source = fmt.Sprintf("git@github.com:%s/%s.git", owner, trimmedName)
	}

	return owner, trimmedName, source, pr, true
}

func resolveCloneTargets(
	ctx context.Context,
	cli *CLI,
	lister repoLister,
) ([]CloneTarget, string, error) {
	repos := cli.Repos
	if len(repos) == 0 && (len(cli.Languages) > 0 || len(cli.Topics) > 0) {
		repos = []string{keywordAll}
	}

	defaultOwner := strings.TrimSpace(cli.Owner)
	requests := make([]repoRequest, 0, len(repos))
	for _, arg := range repos {
		req, err := parseRepoRequest(arg, defaultOwner)
		if err != nil {
			return nil, "", err
		}
		if req.Owner == "" {
			if defaultOwner == "" {
				defaultOwner, err = resolveDefaultOwner()
				if err != nil {
					return nil, "", err
				}
			}
			req.Owner = defaultOwner
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

	baseDir, err := resolveBaseDirectory(cli)
	if err != nil {
		return nil, "", err
	}

	needQuery := requiresRepoQuery(cli, requests)
	repoIndex := make(map[string]map[string]repoInfo)
	if needQuery {
		for _, owner := range requestedOwners(requests) {
			var ownerRepos []repoInfo
			s := clog.Spinner("Fetching").
				Link("owner", "https://github.com/"+owner, owner)
			switch len(cli.Languages) {
			case 0:
			case 1:
				s = s.Str("language", cli.Languages[0])
			default:
				s = s.Strs("languages", cli.Languages)
			}
			if len(cli.TopicFilters) > 0 {
				s = s.Str("topics", formatTopicFilters(cli.TopicFilters))
			}
			listErr := s.Wait(ctx, func(_ context.Context) error {
				var listErr error
				ownerRepos, listErr = lister.ListOwnerRepos(owner, repoListOptions{
					IncludeArchived: cli.Archived,
					IncludeForked:   cli.Forked,
					Visibility:      cli.Visibility,
					Languages:       cli.Languages,
					TopicFilters:    cli.TopicFilters,
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
	for _, req := range selected {
		if req.PullRequest == "" {
			continue
		}
		number, numErr := strconv.Atoi(req.PullRequest)
		if numErr != nil {
			return nil, baseDir, fmt.Errorf(
				"invalid PR number %q for %s/%s",
				req.PullRequest,
				req.Owner,
				req.Name,
			)
		}
		info, resolveErr := lister.ResolvePR(req.Owner, req.Name, number)
		if resolveErr != nil {
			return nil, baseDir, resolveErr
		}
		key := prKey(req)
		useBranch := len(selected) == 1 && prCount == 1 && !info.IsCrossRepository &&
			info.State == prStateOpen
		prMap[key] = prResolution{headRef: info.HeadRefName, useBranch: useBranch}
	}

	targets := make([]CloneTarget, 0, len(selected))
	for _, req := range selected {
		destName := req.Dir
		if destName == "" {
			if cli.Mirror {
				destName = req.Name + ".git"
			} else {
				destName = req.Name
			}
		}

		dest := destName
		if baseDir != "" {
			dest = filepath.Join(baseDir, destName)
		}

		slug := req.Owner + "/" + req.Name

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
			RepoURL:       "https://github.com/" + slug,
			SingleBranch:  cli.Quick,
			Slug:          slug,
			Source:        resolveCloneSource(cli.Method, req),
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

func resolveBaseDirectory(cli *CLI) (string, error) {
	switch {
	case cli.Temp:
		return os.MkdirTemp(os.Getenv(envKeyTmpDir), "clone-*")
	case cli.Directory != "":
		return filepath.Clean(cli.Directory), nil
	default:
		return "", nil
	}
}

func requiresRepoQuery(cli *CLI, requests []repoRequest) bool {
	if len(cli.Languages) > 0 || len(cli.Topics) > 0 || cli.Visibility != keywordAll ||
		cli.Archived ||
		cli.Forked {
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
		part := strings.Join(group, " OR ")
		if len(group) > 1 && len(filters) > 1 {
			part = "(" + part + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " AND ")
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

func resolveCloneSource(method string, req repoRequest) string {
	if req.Source != "" {
		return req.Source
	}
	return cloneURL(method, req.Owner, req.Name)
}

func cloneURL(method, owner, repo string) string {
	if method == methodHTTPS {
		return fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	}
	return fmt.Sprintf("git@github.com:%s/%s.git", owner, repo)
}
