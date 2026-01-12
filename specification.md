# Gitea Runner Operator Specification

## 1. Overview

The Gitea Runner Operator is a Kubernetes controller designed to manage ephemeral Gitea Act runners. It automates the provisioning of runner pods based on the demand of queued jobs in a Gitea instance. By defining `RunnerGroup` resources, users can configure pools of runners with specific scopes (global, organization, or repository) and labels.

## 2. Terminology

- **CRD**: Custom Resource Definition.
- **RunnerGroup CR**: The custom resource instance defining a runner pool.
- **Ephemeral Runner**: A runner that executes exactly one job and then terminates.
- **Gitea Instance**: The target Gitea server where CI/CD workflows are triggered.
- **Runner Capabilities**: The set of labels a runner provides (e.g., `ubuntu-latest`).
- **Job Requirements**: The set of labels a job requests (e.g., `ubuntu-latest`).

## 3. Custom Resource Definition (CRD)

### 3.1 Metadata

- **Group**: `gitea.bpg.pw`
- **Version**: `v1alpha1`
- **Kind**: `RunnerGroup`
- **Scope**: Namespaced

### 3.2 Spec Schema

The `spec` defines the configuration for the runner pool.

| Field               | Type                                   | Required    | Description                                                                                                 |
| :------------------ | :------------------------------------- | :---------- | :---------------------------------------------------------------------------------------------------------- |
| `scope`             | Enum (`global`, `org`, `user`, `repo`) | Yes         | The scope of the runner.                                                                                    |
| `org`               | String                                 | Conditional | The organization name. Required if `scope` is `org`.                                                        |
| `user`              | String                                 | Conditional | The username. Required if `scope` is `user`.                                                                |
| `repo`              | String                                 | Conditional | The repository name. Required if `scope` is `repo`.                                                         |
| `gitea.url`         | String                                 | Yes         | The base URL of the Gitea instance (e.g., `https://gitea.example.com`).                                     |
| `labels`            | []String                               | No          | List of labels for the runner (e.g., `app:infra`). Defaults (e.g. `ubuntu-latest`) are added automatically. |
| `maxActiveRunners`  | Integer                                | Yes         | The maximum number of concurrent runner Jobs allowed for this specific RunnerGroup CR.                      |
| `registrationToken` | SecretKeySelector                      | Yes         | Reference to a Secret containing the runner registration token.                                             |
| `authToken`         | SecretKeySelector                      | Yes         | Reference to a Secret containing an API token to query Gitea for job statuses.                              |

#### 3.2.1 SecretKeySelector

Standard Kubernetes Secret reference:

- `secretRef.name`: Name of the secret.
- `secretRef.key`: Key within the secret containing the value.

### 3.3 Status Schema

- `activeRunners`: Integer. Current count of running Jobs managed by this CR.
- `lastCheckTime`: Timestamp. Last time the controller polled Gitea.

## 4. Controller Logic

### 4.1 Reconciliation Loop

The controller watches for changes to `RunnerGroup` resources.

1.  **Validation**: Ensure `org` or `repo` are present based on `scope`.
2.  **Job List**: List child Jobs to determine `activeRunners` count.
3.  **Status Update**: Update CR status with current metrics.
4.  **Capacity Check**: If `activeRunners >= maxActiveRunners`, stop scaling up.
5.  **Polling**: Fetch job statistics from Gitea.

### 4.2 Polling & Scaling Strategy

The operator uses a robust polling strategy to handle the disconnect between Kubernetes Pod startup time and Gitea's job queue state.

#### 4.2.1 Fetching Stats (`GetRunnerStats`)

The controller queries Gitea for:

1.  **Queued Jobs**: Jobs with status `queued`, `waiting`, or `pending`.
    - **Label Filtering**: Jobs are filtered client-side. A job is considered a match if the RunnerGroup's capabilities (Spec labels + Default labels) are a superset of the Job's required labels.
2.  **Running Jobs**: Jobs with status `running` that belong to this specific runner group (filtered by runner name prefix).

#### 4.2.2 Deduplication Cache (`SpawnedJobsCache`)

To prevent "double scheduling" (where multiple reconciliation loops spawn multiple runners for the same queued job before the first runner can pick it up), the controller maintains an in-memory cache:

- **Key**: Gitea Job ID.
- **Value**: Timestamp when the runner was spawned.
- **TTL**: 5 minutes.

#### 4.2.3 Scaling Algorithm

1.  **Identify Candidates**: Iterate through the list of Queued Jobs from Gitea.
2.  **Check Cache**:
    - If Job ID is in cache and TTL has not expired: **Skip** (Runner already spawned).
    - If Job ID is in cache and TTL expired: **Retry** (Runner likely failed to start).
    - If Job ID is not in cache: **Candidate for spawning**.
3.  **Calculate Slots**: `availableSlots = maxActiveRunners - activeRunners`.
4.  **Spawn**: For each candidate, if `availableSlots > 0`:
    - Create Kubernetes Job.
    - Add Job ID to `SpawnedJobsCache`.
    - Decrement `availableSlots`.
5.  **Cleanup**: Remove Job IDs from the cache if they are no longer present in the Queued Jobs list returned by Gitea (implies they are now Running, Completed, or Cancelled).

## 5. Kubernetes Resource Generation

### 5.1 Job Specification

The controller creates a `batch/v1 Job`.

**Metadata:**

- `name`: `{runnergroup-name}-{random-suffix}`
- `namespace`: Same as `RunnerGroup` CR.
- `labels`:
  - `gitea.bpg.pw/runnergroup-name`: `{runnergroup-name}`
  - `gitea.bpg.pw/managed-by`: `gitea-runner-operator`
- `ownerReferences`: Pointing to the `RunnerGroup` CR.

**Spec:**

- `ttlSecondsAfterFinished`: 600 (Auto-cleanup).
- `template`:
  - `spec`:
    - `restartPolicy`: `OnFailure`
    - `containers`:
      - **Name**: `runner`
      - **Image**: `gitea/act_runner:nightly-dind-rootless`
      - **Env**:
        - `GITEA_INSTANCE_URL`: From `spec.gitea.url`.
        - `GITEA_RUNNER_REGISTRATION_TOKEN`: From Secret.
        - `GITEA_RUNNER_EPHEMERAL`: `"true"`.
        - `GITEA_RUNNER_NAME`: `{job-name}` (Matches Pod name for easier debugging).
        - `GITEA_RUNNER_LABELS`: Comma-separated list of **Effective Labels**.
          - **Effective Labels** = `spec.labels` + Default Gitea Labels (e.g., `ubuntu-latest:docker://node:16-bullseye`, `ubuntu-22.04:...`, etc.) unless explicitly overridden.

## 6. Gitea API Interaction

- **Authentication**: Bearer token provided in `authToken`.
- **Endpoints Used**:
  - `/api/v1/repos/{owner}/{repo}/actions/jobs` (Repo scope)
  - `/api/v1/orgs/{org}/actions/jobs` (Org scope)
  - `/api/v1/users/{user}/repos` + `/api/v1/repos/{owner}/{repo}/actions/jobs` (User scope)
  - `/api/v1/admin/actions/jobs` (Global scope)
- **Label Matching**:
  - The controller implements logic to check: `Job.Labels ⊆ Runner.EffectiveLabels`.
  - Supports both exact matches (`linux`) and schema matches (`ubuntu-latest` matches `ubuntu-latest:docker://...`).

## 7. Security Considerations

- **Token Handling**: Tokens are injected via `valueFrom: secretKeyRef` env vars.
- **Privileged Mode**: `act_runner` dind mode requires privileged security context.
- **Namespace Isolation**: Controller operates within the namespace of the RunnerGroup.
