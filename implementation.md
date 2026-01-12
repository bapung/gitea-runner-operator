# Gitea Runner Operator Implementation Guide

This document outlines the technical implementation details for the Gitea Runner Operator. It is intended for developers building the operator using Go and the Operator SDK.

## 1. Development Environment & Tools

- **Language**: Go (1.21+)
- **Framework**: Operator SDK (v1.33+) with Controller Runtime
- **Kubernetes API**: v1.28+
- **Container Runtime**: Docker or Podman

## 2. Project Initialization

Initialize the project using the Operator SDK:

```bash
operator-sdk init --domain bpg.pw --repo github.com/bapung/gitea-runner-operator
operator-sdk create api --group gitea --version v1alpha1 --kind RunnerGroup --resource --controller
```

## 3. API Definition (`api/v1alpha1/runnergroup_types.go`)

Define the `RunnerGroup` Custom Resource Definition (CRD) in Go structs.

### 3.1 RunnerGroupSpec

```go
type RunnerGroupScope string

const (
    RunnerGroupScopeGlobal RunnerGroupScope = "global"
    RunnerGroupScopeOrg    RunnerGroupScope = "org"
    RunnerGroupScopeUser   RunnerGroupScope = "user"
    RunnerGroupScopeRepo   RunnerGroupScope = "repo"
)

type RunnerGroupSpec struct {
    // Scope defines the scope of the runner (global, org, user, repo)
    // +kubebuilder:validation:Enum=global;org;user;repo
    Scope RunnerScope `json:"scope"`

    // Org is required if scope is 'org'
    // +optional
    Org string `json:"org,omitempty"`

    // User is required if scope is 'user'
    // +optional
    User string `json:"user,omitempty"`

    // Repo is required if scope is 'repo'
    // +optional
    Repo string `json:"repo,omitempty"`

    // GiteaURL is the base URL of the Gitea instance
    GiteaURL string `json:"giteaURL"`

    // Labels to assign to the runner.
    // Defaults (e.g. ubuntu-latest) are merged automatically by the controller.
    // +optional
    Labels []string `json:"labels,omitempty"`

    // MaxActiveRunners is the maximum number of concurrent jobs
    // +kubebuilder:validation:Minimum=1
    MaxActiveRunners int `json:"maxActiveRunners"`

    // RegistrationTokenRef references the secret containing the runner registration token
    RegistrationTokenRef corev1.SecretKeySelector `json:"registrationToken"`

    // AuthTokenRef references the secret containing the Gitea API token for polling
    AuthTokenRef corev1.SecretKeySelector `json:"authToken"`
}
```

### 3.2 RunnerGroupStatus

```go
type RunnerGroupStatus struct {
    // ActiveRunners is the current number of running jobs
    ActiveRunners int `json:"activeRunners"`

    // LastCheckTime is the timestamp of the last poll to Gitea
    LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`
}
```

## 4. Controller Implementation (`internal/controller/runnergroup_controller.go`)

The controller handles the reconciliation loop and manages the lifecycle of ephemeral runners.

### 4.1 Struct Definition

The reconciler includes a thread-safe map to cache spawned jobs and prevent duplicate scheduling.

```go
type RunnerGroupReconciler struct {
    client.Client
    Scheme           *runtime.Scheme
    GiteaClient      gitea.Client
    SpawnedJobsCache sync.Map // Stores [int64]time.Time (JobID -> SpawnTime)
}
```

### 4.2 Reconcile Logic

The `Reconcile` function follows this flow:

1.  **Fetch RunnerGroup**: Get the `RunnerGroup` CR instance.
2.  **List Jobs**: List all `batchv1.Job` resources owned by this CR to calculate `activeRunners`.
3.  **Update Status**: Update `status.activeRunners`.
4.  **Capacity Check**: Stop scaling if `activeRunners >= spec.maxActiveRunners`.
5.  **Label Calculation**: Call `getEffectiveLabels` to merge `spec.labels` with hardcoded Gitea defaults (e.g., `ubuntu-latest:docker://node:16-bullseye`).
6.  **Poll Gitea**:
    - Retrieve Auth Token.
    - Call `GiteaClient.GetRunnerStats` with the effective labels.
    - This returns a list of `QueuedJobs`.
7.  **Scale Up & Deduplication**:
    - Iterate through `stats.QueuedJobs`.
    - **Check Cache**: If Job ID exists in `SpawnedJobsCache`:
      - If TTL (< 5 min) is valid: **Skip** (already handled).
      - If TTL expired: **Retry** (assume previous runner failed).
    - If Job ID not in cache or expired:
      - Check `availableSlots`.
      - Retrieve Registration Token (if not yet fetched).
      - **Spawn Job**: Create `batchv1.Job`.
      - **Update Cache**: Store Job ID in `SpawnedJobsCache`.
      - Decrement `availableSlots`.
8.  **Cache Cleanup**: Remove IDs from `SpawnedJobsCache` if they are not present in the latest `QueuedJobs` list from Gitea.
9.  **Requeue**: Return `ctrl.Result{RequeueAfter: 10 * time.Second}`.

### 4.3 Helper Functions

#### getEffectiveLabels

Merges user-defined labels with Gitea defaults. If a user defines `ubuntu-latest`, it overrides the default `ubuntu-latest:docker://...`.

#### constructJobForRunnerGroup

Creates the Job object with:

- **Name**: `{runnergroup-name}-{random-suffix}`
- **Env**:
  - `GITEA_RUNNER_NAME`: Set to the Job name.
  - `GITEA_RUNNER_LABELS`: Comma-separated effective labels.
  - Standard runner envs (`GITEA_INSTANCE_URL`, etc).

## 5. Gitea Client (`internal/gitea/client.go`)

A specialized client to interact with Gitea's Actions API.

### 5.1 Interface

```go
type RunnerStats struct {
    QueuedJobs []ActionWorkflowJob
    Running    int
}

type Client interface {
    GetRunnerStats(ctx context.Context, giteaURL, authToken string, scope RunnerGroupScope, org, repo string, labels []string) (*RunnerStats, error)
}
```

### 5.2 Logic

1.  **Endpoints**:
    - Repo/Org/Global: Uses `/actions/jobs` endpoints.
    - User: Fetches repos via `/users/{user}/repos`, then queries `/actions/jobs` for each repo.
2.  **Fetching**:
    - Fetches jobs with `status=queued`, `waiting`, `pending`.
    - Handles pagination (fetches all pages).
3.  **Filtering**:
    - Iterates through fetched jobs.
    - **Matches Labels**: Checks if the job's required labels are a subset of the runner's supported labels (effective labels).
      - Supports exact match (`linux` == `linux`)
      - Supports schema match (`ubuntu-latest` matches `ubuntu-latest:docker://...`)
    - Returns only matching jobs in `QueuedJobs`.

## 6. Testing Strategy

1.  **Unit Tests (`internal/gitea/client_test.go`)**:
    - Mock Gitea API server.
    - Verify `GetRunnerStats` correctly parses JSON and handles pagination.
    - Verify label matching logic (subset, schema matching).
2.  **Controller Tests**:
    - Verify `SpawnedJobsCache` prevents double scheduling.
    - Verify TTL logic allows retries for stuck jobs.
    - Verify `getEffectiveLabels` merging logic.
