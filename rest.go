package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	xhttp "github.com/gechr/x/http"
)

const (
	githubAPIBaseURL  = "https://api.github.com"
	githubAPIVersion  = "2022-11-28"
	restPerPage       = 100
	restClientTimeout = 30 * time.Second
	maxErrorBodyBytes = 1 << 16
)

// errNotFound signals a 404 from the REST API so callers can report a precise
// "owner/repo not found" message instead of a generic HTTP error.
var errNotFound = errors.New("not found")

// errAnonRateLimit is returned when an unauthenticated request is rejected for
// exhausting GitHub's anonymous rate limit. The message doubles as the
// user-facing guidance, surfaced through the same path as other lister errors.
var errAnonRateLimit = errors.New(
	"GitHub anonymous API rate limit exceeded (60 requests/hour) - " +
		"set CLONE_GITHUB_TOKEN or run 'gh auth login' to increase to 5000/hour",
)

// restRepoLister implements repoLister against GitHub's REST API without
// authentication. GitHub's GraphQL API has no anonymous tier, so when no token
// is available we fall back to REST, which permits unauthenticated access at a
// reduced rate limit (60 requests/hour). The lower limit is surfaced only if
// actually hit, keeping the tokenless path otherwise seamless.
type restRepoLister struct {
	client  *http.Client
	baseURL string
}

func newRESTRepoLister() *restRepoLister {
	return &restRepoLister{
		client:  &http.Client{Timeout: restClientTimeout},
		baseURL: githubAPIBaseURL,
	}
}

// restRepo mirrors the subset of the REST repository object that maps onto the
// fields repoInfo and the client-side filters need.
type restRepo struct {
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Language   string   `json:"language"`
	Name       string   `json:"name"`
	Visibility string   `json:"visibility"`
	Topics     []string `json:"topics"`
	Stargazers int      `json:"stargazers_count"`
	Archived   bool     `json:"archived"`
	Fork       bool     `json:"fork"`
}

// ListOwnerRepos pages through an owner's repositories. The /users/{owner}
// endpoint serves both user and organization accounts, mirroring GraphQL's
// repositoryOwner, so no account-type detection is needed.
func (l *restRepoLister) ListOwnerRepos(
	ctx context.Context,
	owner string,
	opts repoListOptions,
) ([]repoInfo, error) {
	var repos []repoInfo
	for page := 1; ; page++ {
		path := fmt.Sprintf(
			"/users/%s/repos?type=owner&per_page=%d&page=%d",
			url.PathEscape(owner), restPerPage, page,
		)
		var batch []restRepo
		if err := l.get(ctx, path, &batch); err != nil {
			if errors.Is(err, errNotFound) {
				return nil, fmt.Errorf("could not find GitHub owner %q", owner)
			}
			return nil, fmt.Errorf("querying repositories for %s: %w", owner, err)
		}
		for _, r := range batch {
			if info, ok := repoInfoFromREST(r, owner, opts); ok {
				repos = append(repos, info)
			}
		}
		if len(batch) < restPerPage {
			break
		}
	}
	return repos, nil
}

// ListViewerRepos cannot be served anonymously: "starred"/"watching" are
// relative to an authenticated viewer, so we fail with actionable guidance
// rather than a rate-limit error.
func (l *restRepoLister) ListViewerRepos(
	_ context.Context,
	source viewerSource,
	_ repoListOptions,
) ([]repoInfo, error) {
	return nil, fmt.Errorf(
		"%s repositories require authentication - "+
			"set CLONE_GITHUB_TOKEN or run 'gh auth login'",
		source.label(),
	)
}

// ResolvePR fetches a single pull request. REST reports state in lowercase
// ("open"), so it is upper-cased to match the GraphQL contract (prStateOpen).
func (l *restRepoLister) ResolvePR(
	ctx context.Context,
	owner, repo string,
	number int,
) (prInfo, error) {
	path := fmt.Sprintf(
		"/repos/%s/%s/pulls/%d",
		url.PathEscape(owner), url.PathEscape(repo), number,
	)
	var pr struct {
		State string `json:"state"`
		Head  struct {
			Ref  string `json:"ref"`
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
	}
	if err := l.get(ctx, path, &pr); err != nil {
		if errors.Is(err, errNotFound) {
			return prInfo{}, fmt.Errorf(
				"could not find PR #%d in repository %q",
				number,
				owner+"/"+repo,
			)
		}
		return prInfo{}, fmt.Errorf(
			"querying PR #%d in repository %q: %w",
			number,
			owner+"/"+repo,
			err,
		)
	}
	// A cross-repository PR originates from a fork: its head repo differs from
	// the base repo (a deleted fork yields a nil head repo).
	crossRepo := pr.Head.Repo != nil && pr.Base.Repo != nil &&
		pr.Head.Repo.FullName != pr.Base.Repo.FullName
	return prInfo{
		HeadRefName:       pr.Head.Ref,
		IsCrossRepository: crossRepo,
		State:             strings.ToUpper(pr.State),
	}, nil
}

// repoInfoFromREST applies the same archived/fork/visibility/language/topic/
// stars filtering the GraphQL lister performs, returning false when a repo is
// filtered out.
func repoInfoFromREST(r restRepo, owner string, opts repoListOptions) (repoInfo, bool) {
	if !opts.IncludeArchived && r.Archived {
		return repoInfo{}, false
	}
	if !opts.IncludeForked && r.Fork {
		return repoInfo{}, false
	}

	visibility := strings.ToLower(r.Visibility)
	if opts.Visibility != "" && opts.Visibility != keywordAll && visibility != opts.Visibility {
		return repoInfo{}, false
	}
	if len(opts.Languages) > 0 && !matchesAnyFold(opts.Languages, r.Language) {
		return repoInfo{}, false
	}
	if len(opts.TopicFilters) > 0 && !matchesTopicFilters(opts.TopicFilters, r.Topics...) {
		return repoInfo{}, false
	}
	if opts.Stars.present() && !opts.Stars.matches(r.Stargazers) {
		return repoInfo{}, false
	}

	resolvedOwner := owner
	if r.Owner.Login != "" {
		resolvedOwner = r.Owner.Login
	}
	return repoInfo{
		Language:   r.Language,
		Name:       r.Name,
		Owner:      resolvedOwner,
		Stars:      r.Stargazers,
		Topics:     r.Topics,
		Visibility: visibility,
	}, true
}

// get performs an unauthenticated GET and decodes a 200 response into out.
// It maps 404 to errNotFound and an exhausted anonymous quota to
// errAnonRateLimit; other non-200 responses surface GitHub's error message.
func (l *restRepoLister) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", githubAPIVersion)

	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
		if out == nil {
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(out)
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case isRateLimited(resp):
		return errAnonRateLimit
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if msg := githubErrorMessage(body); msg != "" {
			return fmt.Errorf(
				"GitHub API request failed: %s (%s)",
				msg,
				xhttp.Status(resp.StatusCode),
			)
		}
		return fmt.Errorf("GitHub API request failed (%s)", xhttp.Status(resp.StatusCode))
	}
}

// isRateLimited reports whether a response is a rate-limit rejection (403/429
// with no remaining quota), as opposed to a permission error sharing the 403
// status.
func isRateLimited(resp *http.Response) bool {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return false
	}
	return resp.Header.Get("X-Ratelimit-Remaining") == "0"
}

func githubErrorMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil {
		return e.Message
	}
	return ""
}
