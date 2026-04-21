package main

import (
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
)

const (
	envCloneVCS   = "CLONE_VCS"
	envCloneForge = "CLONE_FORGE"
)

type envConfig struct {
	OwnerAliases map[string]string `env:"OWNER_ALIASES" envKeyValSeparator:"="`
	RepoAliases  map[string]string `env:"REPO_ALIASES"  envKeyValSeparator:"="`
	BinGit       string            `env:"BIN_GIT"`
	BinJJ        string            `env:"BIN_JJ"`
	GitHubToken  string            `env:"GITHUB_TOKEN"`
	Owner        string            `env:"OWNER"`
	TempDir      string            `env:"TMP_DIR"`
}

func loadEnvConfig() (envConfig, error) {
	return env.ParseAsWithOptions[envConfig](env.Options{Prefix: "CLONE_"})
}

func envLower(key string) string {
	return strings.ToLower(strings.TrimSpace(os.Getenv(key)))
}

// lookupAlias finds a key in aliases using smart-case matching: if the key
// contains any uppercase letters, only an exact match is accepted; otherwise
// the lookup is case-insensitive.
func lookupAlias(key string, aliases map[string]string) (string, bool) {
	if v, ok := aliases[key]; ok {
		return v, true
	}
	if key != strings.ToLower(key) {
		return "", false
	}
	for k, v := range aliases {
		if strings.ToLower(k) == key {
			return v, true
		}
	}
	return "", false
}

func resolveOwnerAlias(owner string, aliases map[string]string) string {
	if resolved, ok := lookupAlias(owner, aliases); ok {
		return resolved
	}
	return owner
}

// resolveRepoAlias expands a bare repo alias (no "/") to a full owner/repo,
// preserving any "=dir" or "#pr" suffixes the user appended.
func resolveRepoAlias(arg string, aliases map[string]string) string {
	repoText, dir, hasDir := strings.Cut(arg, "=")
	name, pr, hasPR := strings.Cut(repoText, "#")
	if strings.Contains(name, "/") {
		return arg
	}
	resolved, ok := lookupAlias(name, aliases)
	if !ok {
		return arg
	}
	result := resolved
	if hasPR {
		result += "#" + pr
	}
	if hasDir {
		result += "=" + dir
	}
	return result
}
