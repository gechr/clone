package main

import (
	"fmt"
	"testing"
)

func TestResolveDefaultOwnerUsesEnv(t *testing.T) {
	t.Setenv(envKeyOwner, "oss-owner")

	orig := ghOwnerLookup
	ghOwnerLookup = func() (string, error) {
		t.Fatal("gh lookup should not run when CLONE_OWNER is set")
		return "", nil
	}
	t.Cleanup(func() {
		ghOwnerLookup = orig
	})

	got, err := resolveDefaultOwner()
	if err != nil {
		t.Fatalf("resolveDefaultOwner() error = %v", err)
	}
	if got != "oss-owner" {
		t.Fatalf("resolveDefaultOwner() = %q, want %q", got, "oss-owner")
	}
}

func TestResolveDefaultOwnerFallsBackToGH(t *testing.T) {
	t.Setenv(envKeyOwner, "")

	orig := ghOwnerLookup
	ghOwnerLookup = func() (string, error) {
		return "gh-owner", nil
	}
	t.Cleanup(func() {
		ghOwnerLookup = orig
	})

	got, err := resolveDefaultOwner()
	if err != nil {
		t.Fatalf("resolveDefaultOwner() error = %v", err)
	}
	if got != "gh-owner" {
		t.Fatalf("resolveDefaultOwner() = %q, want %q", got, "gh-owner")
	}
}

func TestResolveDefaultOwnerPropagatesGHError(t *testing.T) {
	t.Setenv(envKeyOwner, "")

	orig := ghOwnerLookup
	ghOwnerLookup = func() (string, error) {
		return "", fmt.Errorf("gh failed")
	}
	t.Cleanup(func() {
		ghOwnerLookup = orig
	})

	_, err := resolveDefaultOwner()
	if err == nil || err.Error() != "gh failed" {
		t.Fatalf("resolveDefaultOwner() error = %v, want %q", err, "gh failed")
	}
}
