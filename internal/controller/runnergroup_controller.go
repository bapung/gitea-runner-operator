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

package controller

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	giteav1alpha1 "github.com/bapung/gitea-runner-operator/api/v1alpha1"
	"github.com/bapung/gitea-runner-operator/internal/gitea"
)

// RunnerGroupReconciler reconciles a RunnerGroup object
type RunnerGroupReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	GiteaClient gitea.Client
}

// +kubebuilder:rbac:groups=gitea.bpg.pw,resources=runnergroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gitea.bpg.pw,resources=runnergroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gitea.bpg.pw,resources=runnergroups/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *RunnerGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch RunnerGroup
	runnerGroup := &giteav1alpha1.RunnerGroup{}
	if err := r.Get(ctx, req.NamespacedName, runnerGroup); err != nil {
		if errors.IsNotFound(err) {
			// RunnerGroup deleted, nothing to do
			logger.Info("RunnerGroup not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get RunnerGroup")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling RunnerGroup", "name", runnerGroup.Name, "namespace", runnerGroup.Namespace)

	// 2. List Jobs owned by this RunnerGroup
	jobList := &batchv1.JobList{}
	labelSelector := client.MatchingLabels{
		"gitea.bpg.pw/runnergroup-name": runnerGroup.Name,
	}
	if err := r.List(ctx, jobList, client.InNamespace(runnerGroup.Namespace), labelSelector); err != nil {
		logger.Error(err, "Failed to list Jobs")
		return ctrl.Result{}, err
	}

	// 3. Update Status - count non-completed jobs
	activeRunners := 0
	for _, job := range jobList.Items {
		// Job is active if it's not completed (no completion time)
		if job.Status.CompletionTime == nil {
			activeRunners++
		}
	}

	// Update status
	runnerGroup.Status.ActiveRunners = activeRunners
	now := metav1.Now()
	runnerGroup.Status.LastCheckTime = &now
	if err := r.Status().Update(ctx, runnerGroup); err != nil {
		logger.Error(err, "Failed to update RunnerGroup status")
		return ctrl.Result{}, err
	}

	logger.Info("Checked active runners", "active", activeRunners, "max", runnerGroup.Spec.MaxActiveRunners)

	// 4. Capacity Check
	if activeRunners >= runnerGroup.Spec.MaxActiveRunners {
		logger.Info("Max active runners reached, skipping scaling",
			"activeRunners", activeRunners,
			"maxActiveRunners", runnerGroup.Spec.MaxActiveRunners)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// 5. Poll Gitea
	// Retrieve Auth Token from Secret
	authToken, err := r.getSecretValue(ctx, runnerGroup.Namespace, runnerGroup.Spec.AuthTokenRef)
	if err != nil {
		logger.Error(err, "Failed to get auth token from secret")
		return ctrl.Result{}, err
	}

	logger.Info("Checking Gitea for queued jobs", "url", runnerGroup.Spec.GiteaURL, "scope", runnerGroup.Spec.Scope)

	// Query for queued workflow runs
	queuedJobs, err := r.GiteaClient.GetQueuedRuns(
		ctx,
		runnerGroup.Spec.GiteaURL,
		authToken,
		runnerGroup.Spec.Scope,
		runnerGroup.Spec.Org,
		runnerGroup.Spec.Repo,
		runnerGroup.Spec.Labels,
	)
	if err != nil {
		logger.Error(err, "Failed to query Gitea for queued runs")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	logger.Info("Gitea query result", "queuedJobs", queuedJobs)

	// 6. Scale Up
	availableSlots := runnerGroup.Spec.MaxActiveRunners - activeRunners
	toSpawn := min(queuedJobs, availableSlots)

	if toSpawn > 0 {
		logger.Info("Spawning runners",
			"queuedJobs", queuedJobs,
			"availableSlots", availableSlots,
			"toSpawn", toSpawn)

		// Retrieve Registration Token from Secret
		registrationToken, err := r.getSecretValue(ctx, runnerGroup.Namespace, runnerGroup.Spec.RegistrationTokenRef)
		if err != nil {
			logger.Error(err, "Failed to get registration token from secret")
			return ctrl.Result{}, err
		}

		// Spawn jobs
		for i := 0; i < toSpawn; i++ {
			job, err := r.constructJobForRunnerGroup(runnerGroup, registrationToken)
			if err != nil {
				logger.Error(err, "Failed to construct Job")
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, job); err != nil {
				logger.Error(err, "Failed to create Job", "jobName", job.Name)
				return ctrl.Result{}, err
			}
			logger.Info("Created Job", "jobName", job.Name)
		}
	}

	// 7. Requeue for continuous polling
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// getSecretValue retrieves a value from a secret
func (r *RunnerGroupReconciler) getSecretValue(ctx context.Context, namespace string, selector corev1.SecretKeySelector) (string, error) {
	secret := &corev1.Secret{}
	secretName := client.ObjectKey{
		Namespace: namespace,
		Name:      selector.Name,
	}

	if err := r.Get(ctx, secretName, secret); err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", selector.Name, err)
	}

	value, ok := secret.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("key %s not found in secret %s", selector.Key, selector.Name)
	}

	return string(value), nil
}

// constructJobForRunnerGroup creates a Job object for the RunnerGroup
func (r *RunnerGroupReconciler) constructJobForRunnerGroup(runnerGroup *giteav1alpha1.RunnerGroup, registrationToken string) (*batchv1.Job, error) {
	// Generate random suffix for name
	name := fmt.Sprintf("%s-%s", runnerGroup.Name, randString(8))

	// Construct Env Vars
	envVars := []corev1.EnvVar{
		{Name: "GITEA_INSTANCE_URL", Value: runnerGroup.Spec.GiteaURL},
		{Name: "GITEA_RUNNER_REGISTRATION_TOKEN", Value: registrationToken},
		{Name: "GITEA_RUNNER_EPHEMERAL", Value: "true"},
		{Name: "DOCKER_HOST", Value: "tcp://localhost:2376"},
		{Name: "DOCKER_CERT_PATH", Value: "/certs/client"},
		{Name: "DOCKER_TLS_VERIFY", Value: "1"},
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
				"gitea.bpg.pw/managed-by":       "gitea-runner-operator",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptr.To(int32(600)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: ptr.To(int64(1000)),
					},
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           "gitea/act_runner:nightly-dind-rootless",
							ImagePullPolicy: corev1.PullAlways,
							SecurityContext: &corev1.SecurityContext{
								Privileged: ptr.To(true),
							},
							Env: envVars,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "runner-data", MountPath: "/data"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "runner-data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
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

// randString generates a random string of the given length
func randString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SetupWithManager sets up the controller with the Manager.
func (r *RunnerGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&giteav1alpha1.RunnerGroup{}).
		Owns(&batchv1.Job{}).
		Named("runnergroup").
		Complete(r)
}
