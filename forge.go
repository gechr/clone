package main

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// forgeConfig describes how to construct clone URLs for a given forge.
type forgeConfig struct {
	GitSuffix bool
	Host      string
	Name      string
}

var forgeRegistry = map[string]forgeConfig{
	forgeBitbucket: {Name: forgeBitbucket, Host: hostBitbucket, GitSuffix: true},
	forgeCodeberg:  {Name: forgeCodeberg, Host: hostCodeberg, GitSuffix: true},
	forgeGitHub:    {Name: forgeGitHub, Host: hostGitHub, GitSuffix: true},
	forgeGitLab:    {Name: forgeGitLab, Host: hostGitLab, GitSuffix: true},
	forgeSourcehut: {Name: forgeSourcehut, Host: hostSourcehut, GitSuffix: false},
}

// resolveForge resolves a forge identifier (enum name or hostname) to its
// config. An empty value defaults to GitHub.
func resolveForge(value string) (forgeConfig, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return forgeRegistry[forgeGitHub], nil
	}
	if cfg, ok := forgeRegistry[v]; ok {
		return cfg, nil
	}
	for _, cfg := range forgeRegistry {
		if cfg.Host == v {
			return cfg, nil
		}
	}
	if strings.Contains(v, ".") {
		return forgeConfig{Name: v, Host: v, GitSuffix: true}, nil
	}
	return forgeConfig{}, fmt.Errorf(
		"invalid forge %q: expected one of bitbucket, codeberg, github, gitlab, sourcehut, or a hostname",
		value,
	)
}

// parseForgeURL attempts to parse a full URL (HTTPS, SSH, or bare hostname)
// into a repoRequest. It dispatches to per-forge extractors based on the
// hostname.
//
// Supported URL forms (per git-clone(1)):
//
//	ssh://[user@]host[:port]/path
//	[user@]host:path              (scp-like)
//	http[s]://host[:port]/path
//	git://host[:port]/path
//	host/path                     (bare hostname)
func parseForgeURL(raw string) (repoRequest, bool) {
	switch {
	case strings.HasPrefix(raw, "ssh://"):
		return parseSchemeURL(raw)
	case strings.HasPrefix(raw, "git://"):
		return parseSchemeURL(raw)
	case strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://"):
		return parseSchemeURL(raw)
	case isSCPLike(raw):
		return parseSCPLike(raw)
	default:
		return parseBareHost(raw)
	}
}

// parseSchemeURL handles any scheme-based URL (https://, ssh://, git://).
func parseSchemeURL(raw string) (repoRequest, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return repoRequest{}, false
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	path := strings.TrimPrefix(parsed.Path, pathSep)
	if parsed.Fragment != "" {
		path += "#" + parsed.Fragment
	}
	return dispatchForge(path, parsed.Scheme, host)
}

// isSCPLike returns true if raw looks like an scp-style remote
// ([user@]host:path). It must contain a colon after the host and
// must not look like an absolute path or scheme-based URL.
func isSCPLike(raw string) bool {
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 {
		return false
	}
	// If there's a slash before the colon, it's not scp-like.
	slash := strings.IndexByte(raw, '/')
	return slash < 0 || colon < slash
}

// parseSCPLike handles [user@]host:path URLs (scp-style).
func parseSCPLike(raw string) (repoRequest, bool) {
	host, path, ok := strings.Cut(raw, ":")
	if !ok || path == "" {
		return repoRequest{}, false
	}
	path = strings.TrimPrefix(path, pathSep)
	// Strip optional user@ prefix from host.
	if _, after, ok := strings.Cut(host, "@"); ok {
		host = after
	}
	return dispatchForge(path, schemeSSH, host)
}

// parseBareHost handles inputs like "github.com/owner/repo".
func parseBareHost(raw string) (repoRequest, bool) {
	slash := strings.IndexByte(raw, '/')
	if slash <= 0 {
		return repoRequest{}, false
	}
	host := raw[:slash]
	if !strings.Contains(host, ".") {
		return repoRequest{}, false
	}
	path := raw[slash+1:]
	return dispatchForge(path, schemeHTTPS, strings.ToLower(host))
}

// dispatchForge routes to the appropriate per-forge parser based on hostname.
func dispatchForge(path, scheme, host string) (repoRequest, bool) {
	switch host {
	case hostGitHub:
		return parseGitHub(path, scheme, host)
	case hostGitLab:
		return parseGitLab(path, scheme, host)
	case hostSourcehut:
		return parseSourcehut(path, scheme, host)
	case hostAzureDevOps:
		return parseAzureDevOps(path, scheme, host)
	default:
		return parseGeneric(path, scheme, host)
	}
}

// splitPath splits a forge URL path into segments, trimming trailing slashes.
func splitPath(path string) []string {
	return strings.Split(strings.TrimSuffix(path, pathSep), pathSep)
}

// forgeResult builds a repoRequest from the extracted components.
func forgeResult(scheme, host, owner, name string, appendDotGit bool) (repoRequest, bool) {
	name = strings.TrimSuffix(name, dotGit)
	if owner == "" || name == "" {
		return repoRequest{}, false
	}
	return repoRequest{
		ExplicitOwner: true,
		Host:          host,
		Owner:         owner,
		Name:          name,
		Source:        forgeSource(scheme, host, owner+pathSep+name, appendDotGit),
	}, true
}

// parseGitHub extracts owner/repo from GitHub paths, with PR support.
//
// Recognized patterns:
//
//	owner/repo
//	owner/repo/pull/42
//	owner/repo#42
//	owner/repo/tree/main/...
func parseGitHub(path, scheme, host string) (repoRequest, bool) {
	clean := strings.TrimSuffix(path, pathSep)

	// Extract fragment-based PR shorthand (owner/repo#42).
	var fragment string
	if before, after, ok := strings.Cut(clean, "#"); ok {
		clean = before
		fragment = after
	}

	segments := splitPath(clean)
	if len(segments) < minRepoSegments {
		return repoRequest{}, false
	}

	var pr string
	if len(segments) >= minPullSegments && segments[2] == "pull" {
		pr = segments[3]
	}
	if pr == "" && fragment != "" {
		pr = fragment
	}

	req, ok := forgeResult(scheme, host, segments[0], segments[1], true)
	if !ok {
		return repoRequest{}, false
	}
	req.PullRequest = pr
	return req, true
}

// parseGitLab extracts owner/repo from GitLab paths, supporting nested groups
// via the /-/ separator.
//
// Recognized patterns:
//
//	owner/repo
//	owner/repo/-/tree/main
//	group/subgroup/repo/-/merge_requests/5
func parseGitLab(path, scheme, host string) (repoRequest, bool) {
	segments := splitPath(path)
	if len(segments) < minRepoSegments {
		return repoRequest{}, false
	}

	var owner, name string
	if dashIdx := slices.Index(segments, "-"); dashIdx >= minRepoSegments {
		// Everything before /-/ is the project path.
		owner = strings.Join(segments[:dashIdx-1], pathSep)
		name = segments[dashIdx-1]
	} else {
		// No /-/ separator: standard 2-segment.
		owner = segments[0]
		name = segments[1]
	}

	return forgeResult(scheme, host, owner, name, true)
}

// parseSourcehut extracts ~owner/repo from Sourcehut paths.
//
// Recognized patterns:
//
//	~owner/repo
//	~owner/repo/tree/main
//	~owner/repo/log/main
func parseSourcehut(path, scheme, host string) (repoRequest, bool) {
	segments := splitPath(path)
	if len(segments) < minRepoSegments {
		return repoRequest{}, false
	}
	return forgeResult(scheme, host, segments[0], segments[1], false)
}

// parseAzureDevOps extracts org/repo from Azure DevOps paths.
//
// Recognized pattern:
//
//	org/project/_git/repo
func parseAzureDevOps(path, scheme, host string) (repoRequest, bool) {
	segments := splitPath(path)

	gitIdx := slices.Index(segments, "_git")
	if gitIdx < 1 || gitIdx+1 >= len(segments) {
		return repoRequest{}, false
	}

	owner := segments[0]
	name := segments[gitIdx+1]
	if owner == "" || name == "" {
		return repoRequest{}, false
	}

	// Azure DevOps clone URLs include the full org/project/_git/repo path.
	repoPath := strings.Join(segments[:gitIdx+2], pathSep)

	return repoRequest{
		ExplicitOwner: true,
		Host:          host,
		Owner:         owner,
		Name:          name,
		Source:        forgeSource(scheme, host, repoPath, false),
	}, true
}

// parseGeneric handles any forge with standard owner/repo paths.
// It takes the first two path segments as owner and repo, discarding
// any trailing UI path segments.
func parseGeneric(path, scheme, host string) (repoRequest, bool) {
	segments := splitPath(path)
	if len(segments) < minRepoSegments {
		return repoRequest{}, false
	}
	return forgeResult(scheme, host, segments[0], segments[1], true)
}

// forgeSource constructs a clone URL for the given scheme, host, and path.
func forgeSource(scheme, host, repoPath string, appendDotGit bool) string {
	suffix := ""
	if appendDotGit {
		suffix = dotGit
	}
	switch scheme {
	case schemeSSH:
		return fmt.Sprintf("git@%s:%s%s", host, repoPath, suffix)
	case schemeGit:
		return fmt.Sprintf("git://%s/%s%s", host, repoPath, suffix)
	default:
		return fmt.Sprintf("https://%s/%s%s", host, repoPath, suffix)
	}
}

// repoWebURL returns the browsable web URL for a repository.
func repoWebURL(host, slug string) string {
	if host == "" {
		host = hostGitHub
	}
	return "https://" + host + pathSep + slug
}
