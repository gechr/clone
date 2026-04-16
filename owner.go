package main

import (
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

var ghOwnerLookup = currentGHOwner

func configuredOwner() string {
	cfg, err := loadEnvConfig()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Owner)
}

func resolveDefaultOwner() (string, error) {
	if owner := configuredOwner(); owner != "" {
		return owner, nil
	}

	owner, err := ghOwnerLookup()
	if err != nil {
		return "", err
	}
	if owner == "" {
		return "", fmt.Errorf("could not determine GitHub owner from gh")
	}
	return owner, nil
}

func currentGHOwner() (string, error) {
	rest, err := api.NewRESTClient(api.ClientOptions{})
	if err != nil {
		return "", fmt.Errorf(
			"owner not specified; set CLONE_OWNER, pass --owner, or install/authenticate GitHub CLI (https://cli.github.com)",
		)
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := rest.Get("user", &user); err != nil {
		return "", fmt.Errorf(
			"owner not specified; set CLONE_OWNER, pass --owner, or authenticate gh: %w",
			err,
		)
	}

	return user.Login, nil
}
