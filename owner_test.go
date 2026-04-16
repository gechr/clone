package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveDefaultOwnerUsesEnv(t *testing.T) {
	t.Setenv("CLONE_OWNER", "oss-owner")

	orig := ghOwnerLookup
	ghOwnerLookup = func() (string, error) {
		require.FailNow(t, "gh lookup should not run when CLONE_OWNER is set")
		return "", nil
	}
	t.Cleanup(func() {
		ghOwnerLookup = orig
	})

	got, err := resolveDefaultOwner()
	require.NoError(t, err)
	require.Equal(t, "oss-owner", got)
}

func TestResolveDefaultOwnerFallsBackToGH(t *testing.T) {
	t.Setenv("CLONE_OWNER", "")

	orig := ghOwnerLookup
	ghOwnerLookup = func() (string, error) {
		return "gh-owner", nil
	}
	t.Cleanup(func() {
		ghOwnerLookup = orig
	})

	got, err := resolveDefaultOwner()
	require.NoError(t, err)
	require.Equal(t, "gh-owner", got)
}

func TestResolveDefaultOwnerPropagatesGHError(t *testing.T) {
	t.Setenv("CLONE_OWNER", "")

	orig := ghOwnerLookup
	ghOwnerLookup = func() (string, error) {
		return "", fmt.Errorf("gh failed")
	}
	t.Cleanup(func() {
		ghOwnerLookup = orig
	})

	_, err := resolveDefaultOwner()
	require.EqualError(t, err, "gh failed")
}
