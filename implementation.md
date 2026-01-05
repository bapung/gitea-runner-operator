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
    RunnerGroupScopeRepo   RunnerGroupScope = "repo"
)

type RunnerGroupSpec struct {
    // Scope defines the scope of the runner (global, org, repo)
    // +kubebuilder:validation:Enum=global;org;repo
    Scope RunnerScope `json:"scope"`

    // Org is required if scope is 'org'
    // +optional
    Org string `json:"org,omitempty"`

    // Repo is required if scope is 'repo'
    // +optional
    Repo string `json:"repo,omitempty"`

    // GiteaURL is the base URL of the Gitea instance
    GiteaURL string `json:"giteaURL"`

    // Labels to assign to the runner
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

The controller handles the reconciliation loop.

### 4.1 RBAC Permissions

Add markers to generate RBAC roles:

```go
// +kubebuilder:rbac:groups=gitea.bpg.pw,resources=runnergroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gitea.bpg.pw,resources=runnergroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
```

### 4.2 Reconcile Logic

The `Reconcile` function should follow this flow:

1.  **Fetch RunnerGroup**: Get the `RunnerGroup` CR instance. If not found, ignore (deleted).
2.  **List Jobs**: List all `batchv1.Job` resources in the same namespace that are owned by this RunnerGroup.
    - Filter by label `gitea.bpg.pw/runnergroup-name=<runnergroup-name>`.
3.  **Update Status**: Update `status.activeRunners` with the count of non-completed jobs.
4.  **Capacity Check**:
    - If `activeRunners >= spec.maxActiveRunners`, stop and requeue.
5.  **Poll Gitea**:
    - Retrieve the Auth Token from the Secret referenced in `spec.authToken`.
    - Instantiate a Gitea API Client.
    - Query for queued workflow runs matching the scope and labels.
6.  **Scale Up**:
    - Calculate `needed = count(queued_jobs)`.
    - Calculate `available_slots = spec.maxActiveRunners - activeRunners`.
    - `to_spawn = min(needed, available_slots)`.
    - Loop `to_spawn` times:
      - Create a new `batchv1.Job`.
7.  **Requeue**: Return `ctrl.Result{RequeueAfter: 10 * time.Second}` to ensure continuous polling.

### 4.3 Job Construction

Helper function to create the Job object:

```go
func (r *RunnerGroupReconciler) constructJobForRunnerGroup(runnerGroup *giteav1alpha1.RunnerGroup, registrationToken string) (*batchv1.Job, error) {
    // Generate random suffix for name
    name := fmt.Sprintf("%s-%s", runnerGroup.Name, randString(5))

    // Construct Env Vars
    envVars := []corev1.EnvVar{
        {Name: "GITEA_INSTANCE_URL", Value: runnerGroup.Spec.GiteaURL},
        {Name: "GITEA_RUNNER_REGISTRATION_TOKEN", Value: registrationToken},
        {Name: "GITEA_RUNNER_EPHEMERAL", Value: "true"},
        {Name: "DOCKER_HOST", Value: "tcp://localhost:2376"},
        // ... other envs from README
    }

    if len(runnerGroup.Spec.Labels) > 0 {
        labelsStr := strings.Join(runnerGroup.Spec.Labels, ",")
        envVars = append(envVars, corev1.EnvVar{Name: "GITEA_RUNNER_LABELS", Value: labelsStr})
    }

    // Construct Job
    job := &batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{
            Name:      name,
            Namespace: runnerGroup.Namespace,
            Labels: map[string]string{
                "app":                           runnerGroup.Name,
                "gitea.bpg.pw/runnergroup-name": runnerGroup.Name,
                "gitea.bpg.pw/managed-by":  "gitea-runner-operator",
            },
        },
        Spec: batchv1.JobSpec{
            TTLSecondsAfterFinished: pointer.Int32(600),
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    RestartPolicy: corev1.RestartPolicyOnFailure,
                    Containers: []corev1.Container{
                        {
                            Name:            "runner",
                            Image:           "gitea/act_runner:nightly-dind-rootless",
                            ImagePullPolicy: corev1.PullAlways,
                            SecurityContext: &corev1.SecurityContext{Privileged: pointer.Bool(true)},
                            Env:             envVars,
                            VolumeMounts: []corev1.VolumeMount{
                                {Name: "runner-data", MountPath: "/data"},
                            },
                        },
                    },
                    Volumes: []corev1.Volume{
                        {
                            Name: "runner-data",
                            VolumeSource: corev1.VolumeSource{
                                PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                                    ClaimName: "act-runner-vol", // Note: Consider making this configurable or EmptyDir
                                },
                            },
                        },
                    },
                },
            },
        },
    }

    // Set Controller Reference
    if err := ctrl.SetControllerReference(runnerGroup, job, r.Scheme); err != nil {
        return nil, err
    }

    return job, nil
}
```

## 5. Gitea Client (`internal/gitea/client.go`)

A simple HTTP client wrapper to interact with Gitea.

### 5.1 Interface

```go
type Client interface {
    GetQueuedRuns(ctx context.Context, scope RunnerGroupScope, owner, repo string, labels []string) (int, error)
}
```

### 5.2 Implementation Details

- **Endpoint**: `/api/v1/repos/{owner}/{repo}/actions/runs`
- **Query Params**: `status=queued`
- **Filtering**:
  - The API might return all queued runs.
  - The client must filter these runs locally to ensure they match the `labels` defined in the RunnerGroup CR.
  - _Note_: Gitea API might not support filtering by labels directly in the list endpoint, so client-side filtering is necessary.

## 6. Configuration & Deployment

### 6.1 Dockerfile

Standard Operator SDK Dockerfile. Ensure the base image is minimal (e.g., `gcr.io/distroless/static:nonroot`).

### 6.2 Kustomize

Update `config/default/kustomization.yaml` to include the CRD and RBAC configurations.

## 7. Testing Strategy

1.  **Unit Tests**:
    - Test `constructJobForRunnerGroup` to ensure Env vars and Labels are set correctly.
    - Test Gitea Client response parsing.
2.  **Integration Tests (EnvTest)**:
    - Spin up a local k8s control plane.
    - Create a `RunnerGroup` CR.
    - Verify the controller creates a `Job` when the mocked Gitea client returns queued jobs.
    - Verify the controller respects `MaxActiveRunners`.
