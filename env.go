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
	cfg, err := env.ParseAsWithOptions[envConfig](env.Options{Prefix: "CLONE_"})
	if err != nil {
		return envConfig{}, err
	}
	cfg.OwnerAliases = normalizeAliasKeys(cfg.OwnerAliases)
	cfg.RepoAliases = normalizeAliasKeys(cfg.RepoAliases)
	return cfg, nil
}

func normalizeAliasKeys(aliases map[string]string) map[string]string {
	normalized := make(map[string]string, len(aliases))
	for k, v := range aliases {
		normalized[strings.ToLower(k)] = v
	}
	return normalized
}

func envLower(key string) string {
	return strings.ToLower(strings.TrimSpace(os.Getenv(key)))
}

func resolveOwnerAlias(owner string, aliases map[string]string) string {
	if resolved, ok := aliases[strings.ToLower(owner)]; ok {
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
	resolved, ok := aliases[strings.ToLower(name)]
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
