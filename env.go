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
	Aliases     map[string]string `env:"OWNER_ALIASES" envKeyValSeparator:"="`
	BinGit      string            `env:"BIN_GIT"`
	BinJJ       string            `env:"BIN_JJ"`
	GitHubToken string            `env:"GITHUB_TOKEN"`
	Owner       string            `env:"OWNER"`
	TempDir     string            `env:"TMP_DIR"`
}

func loadEnvConfig() (envConfig, error) {
	cfg, err := env.ParseAsWithOptions[envConfig](env.Options{Prefix: "CLONE_"})
	if err != nil {
		return envConfig{}, err
	}
	normalized := make(map[string]string, len(cfg.Aliases))
	for k, v := range cfg.Aliases {
		normalized[strings.ToLower(k)] = v
	}
	cfg.Aliases = normalized
	return cfg, nil
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
