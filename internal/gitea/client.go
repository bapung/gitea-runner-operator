/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bapung/gitea-runner-operator/api/v1alpha1"
)

// Client defines the interface for interacting with Gitea API
type Client interface {
	// GetQueuedRuns queries Gitea for queued workflow runs matching the scope and labels
	// Returns the count of queued jobs that match the criteria
	GetQueuedRuns(
		ctx context.Context,
		giteaURL string,
		authToken string,
		scope v1alpha1.RunnerGroupScope,
		org string,
		repo string,
		labels []string,
	) (int, error)
}

// HTTPClient is the default implementation of the Gitea Client interface
type HTTPClient struct {
	httpClient *http.Client
}

// NewHTTPClient creates a new Gitea HTTP client
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Repository represents a Gitea repository
type Repository struct {
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

// Organization represents a Gitea organization
type Organization struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

// ActionWorkflowRunsResponse represents the response structure for workflow runs
type ActionWorkflowRunsResponse struct {
	TotalCount   int64               `json:"total_count"`
	WorkflowRuns []ActionWorkflowRun `json:"workflow_runs"`
}

// ActionWorkflowRun represents a Gitea workflow run
type ActionWorkflowRun struct {
	ID           int64  `json:"id"`
	Status       string `json:"status"`
	DisplayTitle string `json:"display_title"`
	Event        string `json:"event"`
	HeadBranch   string `json:"head_branch"`
	HeadSha      string `json:"head_sha"`
	RunNumber    int64  `json:"run_number"`
}

// ActionWorkflowJobsResponse represents the response structure for workflow jobs
type ActionWorkflowJobsResponse struct {
	TotalCount int64               `json:"total_count"`
	Jobs       []ActionWorkflowJob `json:"jobs"`
}

// ActionWorkflowJob represents a Gitea workflow job with runner labels
type ActionWorkflowJob struct {
	ID         int64    `json:"id"`
	Status     string   `json:"status"`
	Name       string   `json:"name"`
	Labels     []string `json:"labels"`
	RunID      int64    `json:"run_id"`
	RunnerID   int64    `json:"runner_id"`
	RunnerName string   `json:"runner_name"`
}

// GetQueuedRuns implements the Client interface
func (c *HTTPClient) GetQueuedRuns(
	ctx context.Context,
	giteaURL string,
	authToken string,
	scope v1alpha1.RunnerGroupScope,
	org string,
	repo string,
	labels []string,
) (int, error) {
	switch scope {
	case v1alpha1.RunnerGroupScopeRepo:
		return c.getQueuedRunsForRepo(ctx, giteaURL, authToken, org, repo, labels)
	case v1alpha1.RunnerGroupScopeOrg:
		return c.getQueuedRunsForOrg(ctx, giteaURL, authToken, org, labels)
	case v1alpha1.RunnerGroupScopeGlobal:
		return c.getQueuedRunsGlobal(ctx, giteaURL, authToken, labels)
	default:
		return 0, fmt.Errorf("unknown scope: %s", scope)
	}
}

// getQueuedRunsForRepo fetches queued runs for a specific repository
func (c *HTTPClient) getQueuedRunsForRepo(ctx context.Context, giteaURL, authToken, owner, repo string, labels []string) (int, error) {
	// Use jobs endpoint since it contains the runner labels we need for filtering
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/actions/jobs", strings.TrimSuffix(giteaURL, "/"), owner, repo)
	return c.fetchWorkflowJobs(ctx, endpoint, authToken, labels)
}

// getQueuedRunsForOrg fetches queued runs for all repos under an organization
func (c *HTTPClient) getQueuedRunsForOrg(ctx context.Context, giteaURL, authToken, org string, labels []string) (int, error) {
	// Use direct org-level jobs endpoint for better performance
	endpoint := fmt.Sprintf("%s/api/v1/orgs/%s/actions/jobs", strings.TrimSuffix(giteaURL, "/"), org)
	return c.fetchWorkflowJobs(ctx, endpoint, authToken, labels)
}

// getQueuedRunsGlobal fetches queued runs using admin-level API for global scope
func (c *HTTPClient) getQueuedRunsGlobal(ctx context.Context, giteaURL, authToken string, labels []string) (int, error) {
	// Use admin-level jobs endpoint which provides global view of all queued jobs
	endpoint := fmt.Sprintf("%s/api/v1/admin/actions/jobs", strings.TrimSuffix(giteaURL, "/"))
	return c.fetchWorkflowJobs(ctx, endpoint, authToken, labels)
}

// fetchWorkflowJobs fetches workflow jobs from a given endpoint with label filtering and pagination
func (c *HTTPClient) fetchWorkflowJobs(ctx context.Context, endpoint, authToken string, labels []string) (int, error) {
	totalCount := 0
	statuses := []string{"queued", "waiting", "pending"}

	for _, status := range statuses {
		page := 1
		limit := 50 // Default page size

		for {
			u, err := url.Parse(endpoint)
			if err != nil {
				return 0, err
			}
			q := u.Query()
			q.Set("status", status)
			q.Set("page", fmt.Sprintf("%d", page))
			q.Set("limit", fmt.Sprintf("%d", limit))
			u.RawQuery = q.Encode()

			fmt.Printf("DEBUG: Fetching jobs from %s\n", u.String())

			req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
			if err != nil {
				return 0, err
			}

			req.Header.Set("Authorization", "token "+authToken)
			req.Header.Set("Accept", "application/json")

			resp, err := c.httpClient.Do(req)
			if err != nil {
				fmt.Printf("DEBUG: Request failed: %v\n", err)
				return 0, err
			}

			fmt.Printf("DEBUG: Response status: %s\n", resp.Status)

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				fmt.Printf("DEBUG: Error body: %s\n", string(body))
				return 0, c.handleHTTPError(resp.StatusCode, body, "fetch workflow jobs")
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("DEBUG: Response body: %s\n", string(body))

			var result ActionWorkflowJobsResponse
			if err := json.Unmarshal(body, &result); err != nil {
				fmt.Printf("DEBUG: Failed to decode response: %v\n", err)
				return 0, err
			}

			fmt.Printf("DEBUG: Found %d jobs, total in Gitea: %d\n", len(result.Jobs), result.TotalCount)

			// Filter and count matching jobs for this page
			pageCount := c.filterQueuedJobs(result.Jobs, labels)
			fmt.Printf("DEBUG: %d jobs matched labels %v\n", pageCount, labels)
			totalCount += pageCount

			// Break if we've fetched all available results
			if len(result.Jobs) < limit {
				break
			}

			page++
		}
	}

	return totalCount, nil
}

// fetchWorkflowRuns fetches workflow runs from a given endpoint (deprecated - use jobs for label filtering)
func (c *HTTPClient) fetchWorkflowRuns(ctx context.Context, endpoint, authToken string) ([]ActionWorkflowRun, error) {
	// Add status=queued query parameter
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("status", "queued")
	u.RawQuery = q.Encode()

	fmt.Printf("DEBUG: Fetching runs from %s\n", u.String())

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "token "+authToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		fmt.Printf("DEBUG: Request failed: %v\n", err)
		return nil, err
	}
	defer resp.Body.Close()

	fmt.Printf("DEBUG: Response status: %s\n", resp.Status)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("DEBUG: Error body: %s\n", string(body))
		return nil, c.handleHTTPError(resp.StatusCode, body, "fetch workflow runs")
	}

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("DEBUG: Response body: %s\n", string(body))

	var result ActionWorkflowRunsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("DEBUG: Failed to decode response: %v\n", err)
		return nil, err
	}

	return result.WorkflowRuns, nil
}

// fetchOrgRepos fetches all repositories under an organization with pagination
func (c *HTTPClient) fetchOrgRepos(ctx context.Context, giteaURL, authToken, org string) ([]Repository, error) {
	var allRepos []Repository
	page := 1
	limit := 50

	for {
		endpoint := fmt.Sprintf("%s/api/v1/orgs/%s/repos", strings.TrimSuffix(giteaURL, "/"), org)
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("limit", fmt.Sprintf("%d", limit))
		u.RawQuery = q.Encode()

		fmt.Printf("DEBUG: Fetching org repos from %s\n", u.String())

		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "token "+authToken)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			fmt.Printf("DEBUG: Request failed: %v\n", err)
			return nil, err
		}

		fmt.Printf("DEBUG: Response status: %s\n", resp.Status)

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("DEBUG: Error body: %s\n", string(body))
			return nil, c.handleHTTPError(resp.StatusCode, body, "fetch user repos")
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("DEBUG: Response body: %s\n", string(body))

		var repos []Repository
		if err := json.Unmarshal(body, &repos); err != nil {
			fmt.Printf("DEBUG: Failed to decode response: %v\n", err)
			return nil, err
		}

		allRepos = append(allRepos, repos...)

		if len(repos) < limit {
			break
		}

		page++
	}

	return allRepos, nil
}

// fetchAllOrgs fetches all organizations visible to the authenticated user with pagination
func (c *HTTPClient) fetchAllOrgs(ctx context.Context, giteaURL, authToken string) ([]Organization, error) {
	var allOrgs []Organization
	page := 1
	limit := 50

	for {
		endpoint := fmt.Sprintf("%s/api/v1/user/orgs", strings.TrimSuffix(giteaURL, "/"))
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("limit", fmt.Sprintf("%d", limit))
		u.RawQuery = q.Encode()

		fmt.Printf("DEBUG: Fetching all orgs from %s\n", u.String())

		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "token "+authToken)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			fmt.Printf("DEBUG: Request failed: %v\n", err)
			return nil, err
		}

		fmt.Printf("DEBUG: Response status: %s\n", resp.Status)

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("DEBUG: Error body: %s\n", string(body))
			return nil, c.handleHTTPError(resp.StatusCode, body, "fetch org repos")
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("DEBUG: Response body: %s\n", string(body))

		var orgs []Organization
		if err := json.Unmarshal(body, &orgs); err != nil {
			fmt.Printf("DEBUG: Failed to decode response: %v\n", err)
			return nil, err
		}

		allOrgs = append(allOrgs, orgs...)

		if len(orgs) < limit {
			break
		}

		page++
	}

	return allOrgs, nil
}

// fetchUserRepos fetches all repositories owned by the authenticated user with pagination
func (c *HTTPClient) fetchUserRepos(ctx context.Context, giteaURL, authToken string) ([]Repository, error) {
	var allRepos []Repository
	page := 1
	limit := 50

	for {
		endpoint := fmt.Sprintf("%s/api/v1/user/repos", strings.TrimSuffix(giteaURL, "/"))
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("limit", fmt.Sprintf("%d", limit))
		u.RawQuery = q.Encode()

		fmt.Printf("DEBUG: Fetching user repos from %s\n", u.String())

		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "token "+authToken)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			fmt.Printf("DEBUG: Request failed: %v\n", err)
			return nil, err
		}

		fmt.Printf("DEBUG: Response status: %s\n", resp.Status)

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("DEBUG: Error body: %s\n", string(body))
			return nil, c.handleHTTPError(resp.StatusCode, body, "fetch user orgs")
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("DEBUG: Response body: %s\n", string(body))

		var repos []Repository
		if err := json.Unmarshal(body, &repos); err != nil {
			fmt.Printf("DEBUG: Failed to decode response: %v\n", err)
			return nil, err
		}

		allRepos = append(allRepos, repos...)

		if len(repos) < limit {
			break
		}

		page++
	}

	return allRepos, nil
}

// filterQueuedJobs filters workflow jobs by labels
func (c *HTTPClient) filterQueuedJobs(jobs []ActionWorkflowJob, requiredLabels []string) int {
	if len(requiredLabels) == 0 {
		// No label filtering required, return all queued jobs
		return len(jobs)
	}

	count := 0
	for _, job := range jobs {
		match := c.jobMatchesLabels(job.Labels, requiredLabels)
		fmt.Printf("DEBUG: Job %d (Status: %s, Labels: %v) matches requirements %v? %v\n", job.ID, job.Status, job.Labels, requiredLabels, match)
		if match {
			count++
		}
	}
	return count
}

// jobMatchesLabels checks if a job's labels match the required labels
func (c *HTTPClient) jobMatchesLabels(jobLabels, requiredLabels []string) bool {
	// Convert job labels to map for faster lookup
	labelSet := make(map[string]bool)
	for _, label := range jobLabels {
		labelSet[label] = true
	}

	// Check if all required labels are present
	for _, required := range requiredLabels {
		if !labelSet[required] {
			return false
		}
	}
	return true
}

// filterQueuedRuns filters workflow runs by labels (deprecated - use filterQueuedJobs)
func (c *HTTPClient) filterQueuedRuns(runs []ActionWorkflowRun, labels []string) int {
	// Legacy method - jobs should be used for label filtering
	return len(runs)
}

// handleHTTPError provides specific error handling for different HTTP status codes
func (c *HTTPClient) handleHTTPError(statusCode int, body []byte, operation string) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("authentication failed for %s: check your token", operation)
	case http.StatusForbidden:
		return fmt.Errorf("access denied for %s: insufficient permissions", operation)
	case http.StatusNotFound:
		return fmt.Errorf("resource not found for %s: check URL and resource exists", operation)
	case http.StatusTooManyRequests:
		return fmt.Errorf("rate limit exceeded for %s: please retry later", operation)
	case http.StatusInternalServerError:
		return fmt.Errorf("internal server error for %s: %s", operation, string(body))
	default:
		return fmt.Errorf("gitea API returned status %d for %s: %s", statusCode, operation, string(body))
	}
}
