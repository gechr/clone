package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

type repoLister interface {
	ListOwnerRepos(owner string, opts repoListOptions) ([]repoInfo, error)
	ListViewerRepos(source viewerSource, opts repoListOptions) ([]repoInfo, error)
	ResolvePR(owner, repo string, number int) (prInfo, error)
}

type viewerSource int

const (
	viewerStarred viewerSource = iota
	viewerWatching
)

func (s viewerSource) label() string {
	switch s {
	case viewerStarred:
		return "starred"
	case viewerWatching:
		return "watching"
	}
	return ""
}

type repoListOptions struct {
	IncludeArchived bool
	IncludeForked   bool
	Languages       []string
	Stars           rangeFilter
	TopicFilters    [][]string
	Visibility      string
}

type repoInfo struct {
	Language   string
	Name       string
	Owner      string
	Stars      int
	Topics     []string
	Visibility string
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
        stargazerCount
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
						IsArchived bool `json:"isArchived"`
						IsFork     bool `json:"isFork"`
						Language   *struct {
							Name string `json:"name"`
						} `json:"primaryLanguage"`
						Name             string `json:"name"`
						StargazerCount   int    `json:"stargazerCount"`
						Visibility       string `json:"visibility"`
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
			if node.Language != nil {
				language = node.Language.Name
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
			if len(opts.TopicFilters) > 0 && !matchesTopicFilters(opts.TopicFilters, topics...) {
				continue
			}

			if opts.Stars.present() && !opts.Stars.matches(node.StargazerCount) {
				continue
			}

			repos = append(repos, repoInfo{
				Language:   language,
				Name:       node.Name,
				Owner:      owner,
				Stars:      node.StargazerCount,
				Topics:     topics,
				Visibility: visibility,
			})
		}

		if !result.RepositoryOwner.Repositories.PageInfo.HasNextPage {
			break
		}
		cursor = result.RepositoryOwner.Repositories.PageInfo.EndCursor
	}

	return repos, nil
}

func (l *graphQLRepoLister) ListViewerRepos(
	source viewerSource,
	opts repoListOptions,
) ([]repoInfo, error) {
	connection := "starredRepositories"
	if source == viewerWatching {
		connection = "watching"
	}
	query := fmt.Sprintf(`
query($endCursor: String) {
  viewer {
    %s(first: 100, after: $endCursor) {
      nodes {
        name
        owner { login }
        isArchived
        isFork
        visibility
        stargazerCount
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
}`, connection)

	var repos []repoInfo
	var cursor *string
	for {
		var result struct {
			Viewer struct {
				Connection struct {
					Nodes []struct {
						IsArchived bool `json:"isArchived"`
						IsFork     bool `json:"isFork"`
						Language   *struct {
							Name string `json:"name"`
						} `json:"primaryLanguage"`
						Name  string `json:"name"`
						Owner struct {
							Login string `json:"login"`
						} `json:"owner"`
						StargazerCount   int    `json:"stargazerCount"`
						Visibility       string `json:"visibility"`
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
				} `json:"-"`
			} `json:"viewer"`
		}

		raw := map[string]any{}
		if err := l.client.Do(query, map[string]any{"endCursor": cursor}, &raw); err != nil {
			return nil, fmt.Errorf("querying viewer %s: %w", source.label(), err)
		}
		viewer, ok := raw["viewer"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("could not read viewer %s response", source.label())
		}
		connRaw, ok := viewer[connection]
		if !ok {
			return nil, fmt.Errorf("could not read viewer.%s response", connection)
		}
		// Re-decode the specific connection into our typed struct.
		connBytes, err := json.Marshal(connRaw)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(connBytes, &result.Viewer.Connection); err != nil {
			return nil, err
		}

		for _, node := range result.Viewer.Connection.Nodes {
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
			if node.Language != nil {
				language = node.Language.Name
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
			if len(opts.TopicFilters) > 0 && !matchesTopicFilters(opts.TopicFilters, topics...) {
				continue
			}

			if opts.Stars.present() && !opts.Stars.matches(node.StargazerCount) {
				continue
			}

			repos = append(repos, repoInfo{
				Language:   language,
				Name:       node.Name,
				Owner:      node.Owner.Login,
				Stars:      node.StargazerCount,
				Topics:     topics,
				Visibility: visibility,
			})
		}

		if !result.Viewer.Connection.PageInfo.HasNextPage {
			break
		}
		cursor = result.Viewer.Connection.PageInfo.EndCursor
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

func matchesTopicFilters(filters [][]string, candidates ...string) bool {
	for _, group := range filters {
		if !matchesAnyFold(group, candidates...) {
			return false
		}
	}
	return true
}
