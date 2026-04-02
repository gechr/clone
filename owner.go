package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

var ghOwnerLookup = currentGHOwner

func configuredOwner() string {
	return strings.TrimSpace(os.Getenv(envKeyOwner))
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
			"owner not specified; set %s, pass --owner, or install/authenticate gh",
			envKeyOwner,
		)
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := rest.Get("user", &user); err != nil {
		return "", fmt.Errorf(
			"owner not specified; set %s, pass --owner, or authenticate gh: %w",
			envKeyOwner,
			err,
		)
	}

	return user.Login, nil
}
