package main

import (
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

type repoLister interface {
	ListOwnerRepos(owner string, opts repoListOptions) ([]repoInfo, error)
	ResolvePR(owner, repo string, number int) (prInfo, error)
}

type repoListOptions struct {
	IncludeArchived bool
	IncludeForked   bool
	Visibility      string
	Languages       []string
	Topics          []string
}

type repoInfo struct {
	Owner      string
	Name       string
	Visibility string
	Language   string
	Topics     []string
}

type prInfo struct {
	HeadRefName       string
	IsCrossRepository bool
	State             string
}

type graphQLRepoLister struct {
	client *api.GraphQLClient
}

func newGraphQLRepoLister() (*graphQLRepoLister, error) {
	client, err := api.NewGraphQLClient(api.ClientOptions{})
	if err != nil {
		return nil, err
	}
	return &graphQLRepoLister{client: client}, nil
}

func (l *graphQLRepoLister) ListOwnerRepos(owner string, opts repoListOptions) ([]repoInfo, error) {
	const query = `
query OwnerRepos($owner: String!, $endCursor: String) {
  repositoryOwner(login: $owner) {
    repositories(
      first: 100
      after: $endCursor
      orderBy: { field: NAME, direction: ASC }
      affiliations: OWNER
      isFork: null
    ) {
      nodes {
        name
        isArchived
        isFork
        visibility
        primaryLanguage { name }
        repositoryTopics(first: 100) {
          nodes {
            topic { name }
          }
        }
      }
      pageInfo {
        hasNextPage
        endCursor
      }
    }
  }
}`

	var repos []repoInfo
	var cursor *string
	for {
		var result struct {
			RepositoryOwner *struct {
				Repositories struct {
					Nodes []struct {
						Name            string `json:"name"`
						IsArchived      bool   `json:"isArchived"`
						IsFork          bool   `json:"isFork"`
						Visibility      string `json:"visibility"`
						PrimaryLanguage *struct {
							Name string `json:"name"`
						} `json:"primaryLanguage"`
						RepositoryTopics struct {
							Nodes []struct {
								Topic *struct {
									Name string `json:"name"`
								} `json:"topic"`
							} `json:"nodes"`
						} `json:"repositoryTopics"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool    `json:"hasNextPage"`
						EndCursor   *string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"repositories"`
			} `json:"repositoryOwner"`
		}

		vars := map[string]any{"owner": owner, "endCursor": cursor}
		if err := l.client.Do(query, vars, &result); err != nil {
			return nil, fmt.Errorf("querying repositories for %s: %w", owner, err)
		}
		if result.RepositoryOwner == nil {
			return nil, fmt.Errorf("could not find GitHub owner %q", owner)
		}

		for _, node := range result.RepositoryOwner.Repositories.Nodes {
			if !opts.IncludeArchived && node.IsArchived {
				continue
			}
			if !opts.IncludeForked && node.IsFork {
				continue
			}

			visibility := strings.ToLower(node.Visibility)
			if opts.Visibility != "" && opts.Visibility != keywordAll &&
				visibility != opts.Visibility {
				continue
			}

			language := ""
			if node.PrimaryLanguage != nil {
				language = node.PrimaryLanguage.Name
			}
			if len(opts.Languages) > 0 && !matchesAnyFold(opts.Languages, language) {
				continue
			}

			topics := make([]string, 0, len(node.RepositoryTopics.Nodes))
			for _, topicNode := range node.RepositoryTopics.Nodes {
				if topicNode.Topic != nil && topicNode.Topic.Name != "" {
					topics = append(topics, topicNode.Topic.Name)
				}
			}
			if len(opts.Topics) > 0 && !matchesAnyFold(opts.Topics, topics...) {
				continue
			}

			repos = append(repos, repoInfo{
				Owner:      owner,
				Name:       node.Name,
				Visibility: visibility,
				Language:   language,
				Topics:     topics,
			})
		}

		if !result.RepositoryOwner.Repositories.PageInfo.HasNextPage {
			break
		}
		cursor = result.RepositoryOwner.Repositories.PageInfo.EndCursor
	}

	return repos, nil
}

func (l *graphQLRepoLister) ResolvePR(owner, repo string, number int) (prInfo, error) {
	const query = `
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      headRefName
      isCrossRepository
      state
    }
  }
}`

	var result struct {
		Repository *struct {
			PullRequest *struct {
				HeadRefName       string `json:"headRefName"`
				IsCrossRepository bool   `json:"isCrossRepository"`
				State             string `json:"state"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	vars := map[string]any{"owner": owner, "repo": repo, "number": number}
	if err := l.client.Do(query, vars, &result); err != nil {
		return prInfo{}, fmt.Errorf("querying PR #%d in %s/%s: %w", number, owner, repo, err)
	}
	if result.Repository == nil || result.Repository.PullRequest == nil {
		return prInfo{}, fmt.Errorf("could not find PR #%d in %s/%s", number, owner, repo)
	}

	pr := result.Repository.PullRequest
	return prInfo{
		HeadRefName:       pr.HeadRefName,
		IsCrossRepository: pr.IsCrossRepository,
		State:             pr.State,
	}, nil
}

func matchesAnyFold(values []string, candidates ...string) bool {
	for _, value := range values {
		for _, candidate := range candidates {
			if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
				return true
			}
		}
	}
	return false
}
