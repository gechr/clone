package main

import "testing"

func TestCLINormalizeKeepsExplicitOwner(t *testing.T) {
	t.Parallel()

	cli := &CLI{Owner: "cli"}
	cli.Normalize()

	if got, want := cli.Owner, "cli"; got != want {
		t.Fatalf("Owner = %q, want %q", got, want)
	}
}

func TestCLINormalizeMethodHTTPToHTTPS(t *testing.T) {
	t.Parallel()

	cli := &CLI{Method: "http"}
	cli.Normalize()

	if got, want := cli.Method, methodHTTPS; got != want {
		t.Fatalf("Method = %q, want %q", got, want)
	}
}

func TestCLINormalizeMethodHTTPSUnchanged(t *testing.T) {
	t.Parallel()

	cli := &CLI{Method: methodHTTPS}
	cli.Normalize()

	if got, want := cli.Method, methodHTTPS; got != want {
		t.Fatalf("Method = %q, want %q", got, want)
	}
}
