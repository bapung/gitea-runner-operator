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
	// TODO: Add HTTP client and any necessary configuration
}

// NewHTTPClient creates a new Gitea HTTP client
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{}
}

// GetQueuedRuns implements the Client interface
// This is a placeholder implementation that will be fully implemented in step 5
func (c *HTTPClient) GetQueuedRuns(
	ctx context.Context,
	giteaURL string,
	authToken string,
	scope v1alpha1.RunnerGroupScope,
	org string,
	repo string,
	labels []string,
) (int, error) {
	// TODO: Implement actual Gitea API calls
	// This is a placeholder that returns 0 queued jobs

	// Based on scope:
	// - global: Recursively fetch all orgs -> repos -> workflow runs
	// - org: Fetch all repos under org -> workflow runs
	// - repo: Fetch workflow runs for specific repo
	//
	// Endpoint: /api/v1/repos/{owner}/{repo}/actions/runs?status=queued
	// Filter returned runs by labels

	return 0, nil
}
