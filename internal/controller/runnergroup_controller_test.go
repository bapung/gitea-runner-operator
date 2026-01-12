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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	giteav1alpha1 "github.com/bapung/gitea-runner-operator/api/v1alpha1"
	"github.com/bapung/gitea-runner-operator/internal/gitea"
)

type fakeGiteaClient struct{}

func (c *fakeGiteaClient) GetRunnerStats(ctx context.Context, giteaURL, authToken string, scope giteav1alpha1.RunnerGroupScope, org string, user string, repo string, labels []string) (*gitea.RunnerStats, error) {
	return &gitea.RunnerStats{QueuedJobs: []gitea.ActionWorkflowJob{}}, nil
}

var _ = Describe("RunnerGroup Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		runnergroup := &giteav1alpha1.RunnerGroup{}

		BeforeEach(func() {
			By("creating the secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitea-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"token": []byte("dummy"),
					"auth":  []byte("dummy"),
				},
			}
			if err := k8sClient.Create(ctx, secret); err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).To(Succeed())
			}

			By("creating the custom resource for the Kind RunnerGroup")
			err := k8sClient.Get(ctx, typeNamespacedName, runnergroup)
			if err != nil && errors.IsNotFound(err) {
				resource := &giteav1alpha1.RunnerGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: giteav1alpha1.RunnerGroupSpec{
						Scope:            giteav1alpha1.RunnerGroupScopeGlobal,
						GiteaURL:         "https://gitea.example.com",
						MaxActiveRunners: 1,
						RegistrationTokenRef: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "gitea-secret"},
							Key:                  "token",
						},
						AuthTokenRef: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "gitea-secret"},
							Key:                  "auth",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &giteav1alpha1.RunnerGroup{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance RunnerGroup")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &RunnerGroupReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				GiteaClient: &fakeGiteaClient{},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
