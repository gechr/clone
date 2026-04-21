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

func TestResolveOwnerAlias(t *testing.T) {
	aliases := map[string]string{"a": "alpha", "b": "beta"}

	require.Equal(t, "alpha", resolveOwnerAlias("a", aliases))
	require.Equal(t, "A", resolveOwnerAlias("A", aliases)) // uppercase → exact only, no match
	require.Equal(t, "other", resolveOwnerAlias("other", aliases))
	require.Empty(t, resolveOwnerAlias("", aliases))
}

func TestResolveOwnerAliasSmartCase(t *testing.T) {
	aliases := map[string]string{"Foo": "exact", "bar": "lower"}

	// Lowercase input → case-insensitive match.
	require.Equal(t, "exact", resolveOwnerAlias("foo", aliases))
	require.Equal(t, "lower", resolveOwnerAlias("bar", aliases))

	// Uppercase input → exact match only.
	require.Equal(t, "exact", resolveOwnerAlias("Foo", aliases))
	require.Equal(t, "FOO", resolveOwnerAlias("FOO", aliases)) // no exact match → no resolve
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

func TestResolveRepoAlias(t *testing.T) {
	aliases := map[string]string{"x": "alpha/one"}

	require.Equal(t, "alpha/one", resolveRepoAlias("x", aliases))
	require.Equal(t, "X", resolveRepoAlias("X", aliases)) // uppercase → exact only, no match
	require.Equal(t, "alpha/one#21", resolveRepoAlias("x#21", aliases))
	require.Equal(t, "alpha/one=mydir", resolveRepoAlias("x=mydir", aliases))
	require.Equal(t, "alpha/one#21=mydir", resolveRepoAlias("x#21=mydir", aliases))
	require.Equal(t, "unknown", resolveRepoAlias("unknown", aliases))
	require.Equal(t, "owner/repo", resolveRepoAlias("owner/repo", aliases))
}

func TestResolveRepoAliasSmartCase(t *testing.T) {
	aliases := map[string]string{"Foo": "exact-org/repo", "bar": "lower-org/repo"}

	// Lowercase input → case-insensitive match.
	require.Equal(t, "exact-org/repo", resolveRepoAlias("foo", aliases))
	require.Equal(t, "lower-org/repo", resolveRepoAlias("bar", aliases))

	// Uppercase input → exact match only.
	require.Equal(t, "exact-org/repo", resolveRepoAlias("Foo", aliases))
	require.Equal(t, "FOO", resolveRepoAlias("FOO", aliases)) // no exact match → no resolve
}
