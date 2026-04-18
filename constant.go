package main

const (
	minRepoSegments = 2     // owner/repo
	pathSep         = "/"   // URL path separator
	ownerAtMe       = "@me" // GitHub alias for the authenticated user

	keyOwner  = "owner"
	keySource = "source"
	keyStars  = "stars"

	hostAzureDevOps = "dev.azure.com"
	hostBitbucket   = "bitbucket.org"
	hostCodeberg    = "codeberg.org"
	hostGitHub      = "github.com"
	hostGitLab      = "gitlab.com"
	hostSourcehut   = "git.sr.ht"

	forgeBitbucket = "bitbucket"
	forgeCodeberg  = "codeberg"
	forgeGitHub    = "github"
	forgeGitLab    = "gitlab"
	forgeSourcehut = "sourcehut"

	schemeGit   = "git"
	schemeHTTPS = "https"
	schemeSSH   = "ssh"
)
