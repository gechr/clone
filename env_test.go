package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEnvConfigAliases(t *testing.T) {
	t.Setenv("CLONE_OWNER_ALIASES", "a=alpha,b=beta")

	cfg, err := loadEnvConfig()
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"a": "alpha",
		"b": "beta",
	}, cfg.Aliases)
}

func TestLoadEnvConfigAliasesNormalizesKeys(t *testing.T) {
	t.Setenv("CLONE_OWNER_ALIASES", "A=alpha")

	cfg, err := loadEnvConfig()
	require.NoError(t, err)
	require.Equal(t, "alpha", cfg.Aliases["a"])
}

func TestResolveOwnerAlias(t *testing.T) {
	aliases := map[string]string{"a": "alpha", "b": "beta"}

	require.Equal(t, "alpha", resolveOwnerAlias("a", aliases))
	require.Equal(t, "alpha", resolveOwnerAlias("A", aliases))
	require.Equal(t, "other", resolveOwnerAlias("other", aliases))
	require.Empty(t, resolveOwnerAlias("", aliases))
}
