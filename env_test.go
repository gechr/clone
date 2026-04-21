package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEnvConfigOwnerAliases(t *testing.T) {
	t.Setenv("CLONE_OWNER_ALIASES", "a=alpha,b=beta")

	cfg, err := loadEnvConfig()
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"a": "alpha",
		"b": "beta",
	}, cfg.OwnerAliases)
}

func TestLoadEnvConfigOwnerAliasesNormalizesKeys(t *testing.T) {
	t.Setenv("CLONE_OWNER_ALIASES", "A=alpha")

	cfg, err := loadEnvConfig()
	require.NoError(t, err)
	require.Equal(t, "alpha", cfg.OwnerAliases["a"])
}

func TestResolveOwnerAlias(t *testing.T) {
	aliases := map[string]string{"a": "alpha", "b": "beta"}

	require.Equal(t, "alpha", resolveOwnerAlias("a", aliases))
	require.Equal(t, "alpha", resolveOwnerAlias("A", aliases))
	require.Equal(t, "other", resolveOwnerAlias("other", aliases))
	require.Empty(t, resolveOwnerAlias("", aliases))
}

func TestLoadEnvConfigRepoAliases(t *testing.T) {
	t.Setenv("CLONE_REPO_ALIASES", "x=alpha/one,y=beta/two")

	cfg, err := loadEnvConfig()
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"x": "alpha/one",
		"y": "beta/two",
	}, cfg.RepoAliases)
}

func TestLoadEnvConfigRepoAliasesNormalizesKeys(t *testing.T) {
	t.Setenv("CLONE_REPO_ALIASES", "X=alpha/one")

	cfg, err := loadEnvConfig()
	require.NoError(t, err)
	require.Equal(t, "alpha/one", cfg.RepoAliases["x"])
}

func TestResolveRepoAlias(t *testing.T) {
	aliases := map[string]string{"x": "alpha/one"}

	require.Equal(t, "alpha/one", resolveRepoAlias("x", aliases))
	require.Equal(t, "alpha/one", resolveRepoAlias("X", aliases))
	require.Equal(t, "alpha/one#21", resolveRepoAlias("x#21", aliases))
	require.Equal(t, "alpha/one=mydir", resolveRepoAlias("x=mydir", aliases))
	require.Equal(t, "alpha/one#21=mydir", resolveRepoAlias("x#21=mydir", aliases))
	require.Equal(t, "unknown", resolveRepoAlias("unknown", aliases))
	require.Equal(t, "owner/repo", resolveRepoAlias("owner/repo", aliases))
}
