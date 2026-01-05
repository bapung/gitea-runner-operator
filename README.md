# Overview

Operator to manage gitea Act runner on Kubernetes

# How it works?

1. It installs a set of CRDs: `kind: RunnerGroup` in Kubernetes

```yaml
apiVersion: gitea.bpg.pw/v1alpha1
kind: RunnerGroup
metadata:
  name: my-repo-runner-1
  namespace: gitea-runner-system
spec:
  scope: repo
  org: myorg # optional; ommited if scope == global
  repo: myreponame # optional; ommited if scope == org || scope == global
  gitea:
    url: https://gitea.bpg.pw
  labels:
    - default
    - app:infra
  maxActiveRunners: 5 #
  registrationToken: # registration token for runner
    secretRef:
      name: gitea-runner-secret-0
      key: registrationToken
  authToken: # token to get list of job status
    secretRef:
      name: gitea-runner-secret-0
      key: authToken
```

2. The RunnerGroup controller will continuously watch for queued jobs based on its scope: `global`, `org`, or `repo`. If a new workflow run is detected with `status: queued`, based on the RunnerGroup's labels, the controller will spawn a new ephemeral runner as a Job.

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: my-repo-runner-1-275f1b8f
  labels:
    app: my-repo-runner-1
    # tags to determine that this resource is managed by the Operator
spec:
  # Optional: Automatically clean up the job after it finishes (e.g., 100 seconds)
  ttlSecondsAfterFinished: 600
  template:
    metadata:
      labels:
        app: act-my-repo-runner-1
    spec:
      restartPolicy: OnFailure
      securityContext:
        fsGroup: 1000
      volumes:
        - name: runner-data
          persistentVolumeClaim:
            claimName: act-runner-vol
      containers:
        - name: runner
          image: gitea/act_runner:nightly-dind-rootless
          imagePullPolicy: Always
          env:
            - name: DOCKER_HOST
              value: tcp://localhost:2376
            - name: DOCKER_CERT_PATH
              value: /certs/client
            - name: DOCKER_TLS_VERIFY
              value: "1"
            - name: GITEA_INSTANCE_URL
              value: https://gitea.bpg.pw
            - name: GITEA_RUNNER_EPHEMERAL # always ephemeral
              value: "1"
            - name: GITEA_RUNNER_REGISTRATION_TOKEN
              valueFrom:
                secretKeyRef:
                  name: gitea-runner-secret-0
                  key: registrationToken
          securityContext:
            privileged: true
```
