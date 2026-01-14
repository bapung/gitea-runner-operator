# Gitea Runner Operator

A Kubernetes Operator to manage ephemeral Gitea Act runners. This operator automatically spawns runner pods based on queued jobs, support global, org/user, repo level runner. Definetely-vibe-coded (don't worry i know what i am doing).

## Features

- **Ephemeral Runners**: Each job gets a fresh runner which is destroyed after execution.
- **Multiple Scopes**: Support for `global`, `org`, `user`, and `repo` level runners.
- **Auto-Scaling**: Automatically scales runners up to a configured maximum based on queued jobs.
- **Label Matching**: matches Gitea job labels (e.g., `ubuntu-latest`) to runner capabilities.

## Prerequisites

- **Kubernetes Cluster**: v1.23+
- **Gitea**: v1.25.0+ (with Actions enabled)

## Installation (Helm Chart)

You can install the operator using the provided Helm chart.

```bash
# Install the chart
helm upgrade --install gitea-runner-operator ./charts/gitea-runner-operator \
  --namespace gitea-runner-operator-system \
  --create-namespace
```

## Installation (Manual)

### 1. Deploy the Operator

You can deploy the operator using the provided manifests.

```bash
# Clone the repository
git clone https://github.com/bapung/gitea-runner-operator.git
cd gitea-runner-operator

# Install CRDs
make install

# Deploy the controller to the cluster
make deploy IMG=ghcr.io/bapung/gitea-runner-operator:latest
```

### 2. Create Credentials Secret

Create a secret containing the Gitea Registration Token and an API Auth Token.

1.  **Registration Token**: Get this from Gitea Admin -> Actions -> Runners -> Create new Runner (or Org/Repo settings).
2.  **Auth Token**: Generate a token in Gitea User Settings -> Applications. It needs `read:repository`, `read:user` permissions.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: gitea-runner-secret
  namespace: gitea-runner-operator-system
type: Opaque
stringData:
  registrationToken: "<YOUR_REGISTRATION_TOKEN>"
  authToken: "<YOUR_API_TOKEN>"
```

Apply it:

```bash
kubectl apply -f secret.yaml
```

## Configuration

The core resource is the `RunnerGroup`. Use the `scope` field to control which jobs the runner picks up.

```yaml
apiVersion: gitea.bpg.pw/v1alpha1
kind: RunnerGroup
metadata:
  name: example-runner
  namespace: gitea-runner-operator-system
spec:
  # Base Gitea URL
  giteaURL: https://gitea.example.com

  # Scope configuration:
  # - global: Runs all jobs (requires instance admin token)
  # - org:    Runs jobs for a specific organization (requires `org` field)
  # - user:   Runs jobs for a specific user (requires `user` field)
  # - repo:   Runs jobs for a specific repository (requires `org` and `repo` fields)
  scope: repo

  # Scope-specific fields (uncomment as needed based on scope):
  org: my-org # Required for 'org' or 'repo' scopes
  repo: my-repo # Required for 'repo' scope
  # user: my-user   # Required for 'user' or 'repo' scope

  # Maximum concurrent runners
  maxActiveRunners: 5

  # Labels supported by this runner group
  labels:
    - "ubuntu-latest"
    - "custom-label:docker://node:16"

  # Credentials
  registrationToken:
    secretRef:
      name: gitea-runner-secret
      key: registrationToken
  authToken:
    secretRef:
      name: gitea-runner-secret
      key: authToken
```

## How it works

1.  The **Controller** polls the Gitea API (using the `authToken`) to check for queued jobs matching the scope and labels.
2.  If a matching queued job is found, and the current active runner count is below `maxActiveRunners`, the Controller creates a Kubernetes `Job`.
3.  The `Job` pod starts an `act_runner` instance, registers itself using the `registrationToken` (as ephemeral), picks up the job, executes it, and then terminates.

## Troubleshooting

### Runners are not starting

1.  **Check Controller Logs**:

    ```bash
    kubectl logs -n gitea-runner-operator-system -l control-plane=controller-manager -f
    ```

    Look for errors regarding API authentication or connectivity.

2.  **Check Permissions**:
    Ensure the `authToken` has sufficient permissions (`read:repository`, etc.) to query actions.

3.  **Check Labels**:
    Enable debug logging in the controller to see label matching logic. If your Gitea job requires `ubuntu-latest` but your RunnerGroup defines `centos`, it won't match.

### Docker Daemon Issues

This is a default rootless Job template from Gitea doc, it has issues with docker daemon. I still can't to get it working with `docker` command, other container works just fine if you put correct labels.
Per Gemini:
The default runner image uses `dind-rootless`. This requires the pod to run with `privileged: true`. Ensure your cluster policies (PSP/PSA) allow privileged pods in the operator namespace.

## Roadmap / Wishlist

- Helm Chart
- Custom Runner Job Spec definition
- Push mode using Webhook trigger

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
