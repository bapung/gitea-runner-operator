# Gitea Runner Operator Specification

## 1. Overview

The Gitea Runner Operator is a Kubernetes controller designed to manage ephemeral Gitea Act runners. It automates the provisioning of runner pods based on the demand of queued jobs in a Gitea instance. By defining `RunnerGroup` resources, users can configure pools of runners with specific scopes (global, organization, or repository) and labels.

## 2. Terminology

- **CRD**: Custom Resource Definition.
- **RunnerGroup CR**: The custom resource instance defining a runner pool.
- **Ephemeral Runner**: A runner that executes exactly one job and then terminates.
- **Gitea Instance**: The target Gitea server where CI/CD workflows are triggered.

## 3. Custom Resource Definition (CRD)

### 3.1 Metadata

- **Group**: `gitea.bpg.pw`
- **Version**: `v1alpha1`
- **Kind**: `RunnerGroup`
- **Scope**: Namespaced

### 3.2 Spec Schema

The `spec` defines the configuration for the runner pool.

| Field               | Type                           | Required    | Description                                                                                                 |
| :------------------ | :----------------------------- | :---------- | :---------------------------------------------------------------------------------------------------------- |
| `scope`             | Enum (`global`, `org`, `repo`) | Yes         | The scope of the runner.                                                                                    |
| `org`               | String                         | Conditional | The organization name. Required if `scope` is `org`.                                                        |
| `repo`              | String                         | Conditional | The repository name. Required if `scope` is `repo`.                                                         |
| `gitea.url`         | String                         | Yes         | The base URL of the Gitea instance (e.g., `https://gitea.example.com`).                                     |
| `labels`            | []String                       | No          | List of labels for the runner (e.g., `ubuntu-latest`, `app:infra`). Used by Gitea to match jobs to runners. |
| `maxActiveRunners`  | Integer                        | Yes         | The maximum number of concurrent runner Jobs allowed for this specific RunnerGroup CR.                      |
| `registrationToken` | SecretKeySelector              | Yes         | Reference to a Secret containing the runner registration token.                                             |
| `authToken`         | SecretKeySelector              | Yes         | Reference to a Secret containing an API token to query Gitea for job statuses.                              |

#### 3.2.1 SecretKeySelector

Standard Kubernetes Secret reference:

- `secretRef.name`: Name of the secret.
- `secretRef.key`: Key within the secret containing the value.

### 3.3 Status Schema (Optional but Recommended)

- `activeRunners`: Integer. Current count of running Jobs managed by this CR.
- `lastCheckTime`: Timestamp. Last time the controller polled Gitea.

## 4. Controller Logic

### 4.1 Reconciliation Loop

The controller watches for changes to `RunnerGroup` resources.

1.  **Validation**: Ensure `org` or `repo` are present based on `scope`.
2.  **Job Cleanup**: (Optional) Check for and remove "stuck" jobs if TTL doesn't cover edge cases, though `ttlSecondsAfterFinished` is primary.
3.  **Metric Collection**: Update status with current running job count.
4.  **Polling**: The controller must implement a polling mechanism (loop) independent of the standard Reconcile trigger, or requeue the Reconcile event periodically (e.g., every 10-30 seconds).

### 4.2 Polling & Scaling Logic

On every poll interval for a specific `RunnerGroup` CR:

1.  **Check Capacity**:
    - Query Kubernetes for active `Jobs` owned by this `RunnerGroup` CR.
    - If `count(active_jobs) >= maxActiveRunners`, stop. Do not spawn new runners.

2.  **Fetch Queued Jobs**:
    - Call Gitea API using `authToken`.
    - Endpoint depends on scope:
      - **Global**: Recursively fetch all workflow runs:
        1. Fetch all organizations in the Gitea instance
        2. For each organization, fetch all repositories under that org
        3. For each repository, query `/repos/{owner}/{repo}/actions/runs?status=queued`
        4. Additionally, fetch all user-owned repositories and query their workflow runs
      - **Org**: Fetch all workflow runs in repos under the organization:
        1. Fetch all repositories under the specified organization
        2. For each repository, query `/repos/{owner}/{repo}/actions/runs?status=queued`
      - **Repo**: Directly query `/repos/{owner}/{repo}/actions/runs?status=queued`
    - Filter the returned runs:
      - Must match the `labels` defined in the `RunnerGroup` CR.

3.  **Spawn Runner**:
    - If a queued job is found and capacity allows, create a Kubernetes `Job`.
    - **One Job per Queued Workflow**: Ideally, the logic should map 1 queued run -> 1 Runner Job.
    - **Concurrency Control**: Ensure we don't spawn more jobs than `maxActiveRunners - currentActiveRunners`.

## 5. Kubernetes Resource Generation

### 5.1 Job Specification

The controller creates a `batch/v1 Job`.

**Metadata:**

- `name`: `{runnergroup-cr-name}-{random-suffix}`
- `namespace`: Same as `RunnerGroup` CR.
- `labels`:
  - `app`: `{runnergroup-cr-name}`
  - `gitea.bpg.pw/managed-by`: `gitea-runner-operator`
  - `gitea.bpg.pw/runnergroup-name`: `{runnergroup-cr-name}`
- `ownerReferences`: Pointing to the `RunnerGroup` CR.

**Spec:**

- `ttlSecondsAfterFinished`: 600 (Clean up finished jobs).
- `template`:
  - `spec`:
    - `restartPolicy`: `OnFailure`
    - `containers`:
      - **Name**: `runner`
      - **Image**: `gitea/act_runner:nightly-dind-rootless` (Default, potentially configurable in CR later).
      - **SecurityContext**: `privileged: true` (Required for DIND).
      - **Env**:
        - `GITEA_INSTANCE_URL`: From `spec.gitea.url`.
        - `GITEA_RUNNER_REGISTRATION_TOKEN`: From `spec.registrationToken`.
        - `GITEA_RUNNER_EPHEMERAL`: `"true"`.
        - `GITEA_RUNNER_LABELS`: Comma-separated list from `spec.labels`.
        - `DOCKER_HOST`: `tcp://localhost:2376`
      - **VolumeMounts**:
        - Mount docker socket or storage if necessary. The README example uses a PVC `act-runner-vol` mounted to `/data`. _Note: Using a shared PVC for ephemeral runners might cause race conditions. EmptyDir is preferred for truly ephemeral runners unless caching is strictly required and managed._

## 6. Gitea API Interaction

- **Authentication**: Bearer token provided in `authToken`.
- **Client**: HTTP Client with timeout.

## 7. Security Considerations

- **Token Handling**: Registration and Auth tokens are read from Kubernetes Secrets and injected as Environment Variables. They are not stored in plain text in the CR.
- **Privileged Mode**: The default `act_runner` image (dind) requires privileged mode. The Operator creates Jobs with this permission.
- **Namespace Isolation**: The Operator should respect RBAC and only operate within allowed namespaces.
