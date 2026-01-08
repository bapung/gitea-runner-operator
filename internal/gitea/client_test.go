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

func TestHTTPClient_GetQueuedRuns(t *testing.T) {
	tests := []struct {
		name          string
		scope         v1alpha1.RunnerGroupScope
		org           string
		repo          string
		labels        []string
		mockResponse  ActionWorkflowJobsResponse
		expectedCount int
		expectedError bool
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
			expectedCount: 1,
			expectedError: false,
		},
		{
			name:   "org scope no label filtering",
			scope:  v1alpha1.RunnerGroupScopeOrg,
			org:    "testorg",
			labels: []string{},
			mockResponse: ActionWorkflowJobsResponse{
				TotalCount: 3,
				Jobs: []ActionWorkflowJob{
					{ID: 1, Status: "queued", Labels: []string{"linux", "x64"}},
					{ID: 2, Status: "queued", Labels: []string{"windows"}},
					{ID: 3, Status: "queued", Labels: []string{"macos"}},
				},
			},
			expectedCount: 3,
			expectedError: false,
		},
		{
			name:   "global scope with specific labels",
			scope:  v1alpha1.RunnerGroupScopeGlobal,
			labels: []string{"docker"},
			mockResponse: ActionWorkflowJobsResponse{
				TotalCount: 2,
				Jobs: []ActionWorkflowJob{
					{ID: 1, Status: "queued", Labels: []string{"docker", "linux"}},
					{ID: 2, Status: "queued", Labels: []string{"linux"}},
				},
			},
			expectedCount: 1,
			expectedError: false,
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

				// Verify query parameters
				if r.URL.Query().Get("status") != "queued" {
					t.Errorf("Expected status=queued, got %s", r.URL.Query().Get("status"))
				}

				// Verify authorization header
				authHeader := r.Header.Get("Authorization")
				if !strings.HasPrefix(authHeader, "token ") {
					t.Errorf("Expected Authorization header to start with 'token ', got %s", authHeader)
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			client := NewHTTPClient()
			count, err := client.GetQueuedRuns(
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
			if count != tt.expectedCount {
				t.Errorf("Expected count %d, got %d", tt.expectedCount, count)
			}
		})
	}
}

func TestJobMatchesLabels(t *testing.T) {
	client := &HTTPClient{}

	tests := []struct {
		name           string
		jobLabels      []string
		requiredLabels []string
		expected       bool
	}{
		{
			name:           "exact match",
			jobLabels:      []string{"linux", "x64"},
			requiredLabels: []string{"linux", "x64"},
			expected:       true,
		},
		{
			name:           "subset match",
			jobLabels:      []string{"linux", "x64", "docker"},
			requiredLabels: []string{"linux", "x64"},
			expected:       true,
		},
		{
			name:           "no match",
			jobLabels:      []string{"linux", "arm64"},
			requiredLabels: []string{"linux", "x64"},
			expected:       false,
		},
		{
			name:           "empty required labels",
			jobLabels:      []string{"linux", "x64"},
			requiredLabels: []string{},
			expected:       true,
		},
		{
			name:           "partial match",
			jobLabels:      []string{"linux"},
			requiredLabels: []string{"linux", "x64"},
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.jobMatchesLabels(tt.jobLabels, tt.requiredLabels)
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
		name           string
		requiredLabels []string
		expectedCount  int
	}{
		{
			name:           "filter by linux",
			requiredLabels: []string{"linux"},
			expectedCount:  3,
		},
		{
			name:           "filter by linux and x64",
			requiredLabels: []string{"linux", "x64"},
			expectedCount:  2,
		},
		{
			name:           "filter by docker",
			requiredLabels: []string{"docker"},
			expectedCount:  1,
		},
		{
			name:           "no labels - return all",
			requiredLabels: []string{},
			expectedCount:  4,
		},
		{
			name:           "no matches",
			requiredLabels: []string{"macos"},
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := client.filterQueuedJobs(jobs, tt.requiredLabels)
			if count != tt.expectedCount {
				t.Errorf("Expected %d, got %d", tt.expectedCount, count)
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
