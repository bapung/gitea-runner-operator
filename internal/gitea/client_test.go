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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bapung/gitea-runner-operator/api/v1alpha1"
)

func TestHTTPClient_GetRunnerStats(t *testing.T) {
	tests := []struct {
		name           string
		scope          v1alpha1.RunnerGroupScope
		org            string
		repo           string
		labels         []string
		mockResponse   ActionWorkflowJobsResponse
		expectedQueued int
		expectedError  bool
	}{
		{
			name:   "repo scope with matching labels",
			scope:  v1alpha1.RunnerGroupScopeRepo,
			org:    "testorg",
			repo:   "testrepo",
			labels: []string{"linux", "x64"},
			mockResponse: ActionWorkflowJobsResponse{
				TotalCount: 2,
				Jobs: []ActionWorkflowJob{
					{ID: 1, Status: "queued", Labels: []string{"linux", "x64"}},
					{ID: 2, Status: "queued", Labels: []string{"linux", "arm64"}},
				},
			},
			expectedQueued: 1, // Job 1 matches
			expectedError:  false,
		},
		{
			name:   "org scope no label filtering (matches all)",
			scope:  v1alpha1.RunnerGroupScopeOrg,
			org:    "testorg",
			labels: []string{}, // No specific capabilities, matches jobs with empty requirements? No, empty labels matches nothing?
			// Wait, previous logic was: if reqLabels is empty, return all.
			// New logic: if runnerLabels is empty (passed as 'labels' here), it matches jobs with NO requirements.
			// But for test purposes, let's assume we pass runner capabilities.
			// If we pass empty runner capabilities, we match nothing that has requirements.
			// Let's pass capabilities that cover the jobs.
			mockResponse: ActionWorkflowJobsResponse{
				TotalCount: 3,
				Jobs: []ActionWorkflowJob{
					{ID: 1, Status: "queued", Labels: []string{"linux"}},
				},
			},
			expectedQueued: 0, // No runner capabilities provided -> no match
			expectedError:  false,
		},
		{
			name:   "global scope with specific labels",
			scope:  v1alpha1.RunnerGroupScopeGlobal,
			labels: []string{"docker", "linux"},
			mockResponse: ActionWorkflowJobsResponse{
				TotalCount: 2,
				Jobs: []ActionWorkflowJob{
					{ID: 1, Status: "queued", Labels: []string{"docker", "linux"}}, // Match
					{ID: 2, Status: "queued", Labels: []string{"linux"}},           // Match (subset)
				},
			},
			expectedQueued: 2,
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify correct endpoint is called
				expectedPath := ""
				switch tt.scope {
				case v1alpha1.RunnerGroupScopeRepo:
					expectedPath = "/api/v1/repos/testorg/testrepo/actions/jobs"
				case v1alpha1.RunnerGroupScopeOrg:
					expectedPath = "/api/v1/orgs/testorg/actions/jobs"
				case v1alpha1.RunnerGroupScopeGlobal:
					expectedPath = "/api/v1/admin/actions/jobs"
				}

				if !strings.HasPrefix(r.URL.Path, expectedPath) {
					t.Errorf("Expected path to start with %s, got %s", expectedPath, r.URL.Path)
				}

				// Verify authorization header
				authHeader := r.Header.Get("Authorization")
				if !strings.HasPrefix(authHeader, "token ") {
					t.Errorf("Expected Authorization header to start with 'token ', got %s", authHeader)
				}

				w.Header().Set("Content-Type", "application/json")

				// Only return jobs for 'queued' status to simplify counting
				if r.URL.Query().Get("status") == "queued" {
					json.NewEncoder(w).Encode(tt.mockResponse)
				} else {
					json.NewEncoder(w).Encode(ActionWorkflowJobsResponse{TotalCount: 0, Jobs: []ActionWorkflowJob{}})
				}
			}))
			defer server.Close()

			client := NewHTTPClient()
			stats, err := client.GetRunnerStats(
				context.Background(),
				server.URL,
				"test-token",
				tt.scope,
				tt.org,
				tt.repo,
				tt.labels,
			)

			if tt.expectedError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			if stats != nil {
				if len(stats.QueuedJobs) != tt.expectedQueued {
					t.Errorf("Expected %d queued jobs, got %d", tt.expectedQueued, len(stats.QueuedJobs))
				}
			}
		})
	}
}

func TestJobMatchesLabels(t *testing.T) {
	client := &HTTPClient{}

	tests := []struct {
		name            string
		jobLabels       []string
		supportedLabels []string
		expected        bool
	}{
		{
			name:            "exact match",
			jobLabels:       []string{"linux", "x64"},
			supportedLabels: []string{"linux", "x64"},
			expected:        true,
		},
		{
			name:            "subset match (runner has more)",
			jobLabels:       []string{"linux"},
			supportedLabels: []string{"linux", "x64"},
			expected:        true,
		},
		{
			name:            "schema match",
			jobLabels:       []string{"ubuntu-latest"},
			supportedLabels: []string{"ubuntu-latest:docker://node:16"},
			expected:        true,
		},
		{
			name:            "no match (missing req)",
			jobLabels:       []string{"linux", "arm64"},
			supportedLabels: []string{"linux", "x64"},
			expected:        false,
		},
		{
			name:            "empty required labels (matches anything)",
			jobLabels:       []string{},
			supportedLabels: []string{"linux"},
			expected:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.jobMatchesLabels(tt.jobLabels, tt.supportedLabels)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestFilterQueuedJobs(t *testing.T) {
	client := &HTTPClient{}

	jobs := []ActionWorkflowJob{
		{ID: 1, Labels: []string{"linux", "x64"}},
		{ID: 2, Labels: []string{"linux", "arm64"}},
		{ID: 3, Labels: []string{"windows", "x64"}},
		{ID: 4, Labels: []string{"linux", "x64", "docker"}},
	}

	tests := []struct {
		name            string
		supportedLabels []string
		expectedIDs     []int64
	}{
		{
			name:            "runner supports linux, x64",
			supportedLabels: []string{"linux", "x64"},
			expectedIDs:     []int64{1},
		},
		{
			name:            "runner supports linux, x64, docker",
			supportedLabels: []string{"linux", "x64", "docker"},
			expectedIDs:     []int64{1, 4},
		},
		{
			name:            "runner supports everything",
			supportedLabels: []string{"linux", "x64", "arm64", "windows", "docker"},
			expectedIDs:     []int64{1, 2, 3, 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := client.filterQueuedJobs(jobs, tt.supportedLabels)
			if len(matched) != len(tt.expectedIDs) {
				t.Errorf("Expected %d matched jobs, got %d", len(tt.expectedIDs), len(matched))
			}
		})
	}
}

func TestHandleHTTPError(t *testing.T) {
	client := &HTTPClient{}

	tests := []struct {
		name        string
		statusCode  int
		body        []byte
		operation   string
		expectedErr string
	}{
		{
			name:        "unauthorized",
			statusCode:  401,
			body:        []byte("Unauthorized"),
			operation:   "test operation",
			expectedErr: "authentication failed for test operation: check your token",
		},
		{
			name:        "forbidden",
			statusCode:  403,
			body:        []byte("Forbidden"),
			operation:   "test operation",
			expectedErr: "access denied for test operation: insufficient permissions",
		},
		{
			name:        "not found",
			statusCode:  404,
			body:        []byte("Not Found"),
			operation:   "test operation",
			expectedErr: "resource not found for test operation: check URL and resource exists",
		},
		{
			name:        "rate limit",
			statusCode:  429,
			body:        []byte("Too Many Requests"),
			operation:   "test operation",
			expectedErr: "rate limit exceeded for test operation: please retry later",
		},
		{
			name:        "server error",
			statusCode:  500,
			body:        []byte("Internal Server Error"),
			operation:   "test operation",
			expectedErr: "internal server error for test operation: Internal Server Error",
		},
		{
			name:        "other error",
			statusCode:  400,
			body:        []byte("Bad Request"),
			operation:   "test operation",
			expectedErr: "gitea API returned status 400 for test operation: Bad Request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.handleHTTPError(tt.statusCode, tt.body, tt.operation)
			if err.Error() != tt.expectedErr {
				t.Errorf("Expected error %q, got %q", tt.expectedErr, err.Error())
			}
		})
	}
}
