package main

// Host, forge, and scheme names live in github.com/gechr/forge - referencing
// them there rather than redeclaring them here keeps one source of truth, so a
// change upstream cannot leave clone asserting a stale value.
const (
	pathSep = "/" // URL path separator
	dotGit  = ".git"
	atMe    = "@me" // GitHub alias for the authenticated user

	keyOwner  = "owner"
	keySource = "source"
	keyStars  = "stars"
)
