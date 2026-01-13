/*
Copyright 2026 bapung.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RunnerGroupScope defines the scope of the runner group
type RunnerGroupScope string

const (
	// RunnerGroupScopeGlobal means the runner group is available globally
	RunnerGroupScopeGlobal RunnerGroupScope = "global"
	// RunnerGroupScopeOrg means the runner group is scoped to an organization
	RunnerGroupScopeOrg RunnerGroupScope = "org"
	// RunnerGroupScopeUser means the runner group is scoped to a user
	RunnerGroupScopeUser RunnerGroupScope = "user"
	// RunnerGroupScopeRepo means the runner group is scoped to a repository
	RunnerGroupScopeRepo RunnerGroupScope = "repo"
)

// RunnerGroupSpec defines the desired state of RunnerGroup.
type RunnerGroupSpec struct {
	// Scope defines the scope of the runner (global, org, user, repo)
	// +kubebuilder:validation:Enum=global;org;user;repo
	// +kubebuilder:validation:Required
	Scope RunnerGroupScope `json:"scope"`

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
	// +kubebuilder:validation:Required
	GiteaURL string `json:"giteaURL"`

	// Labels to assign to the runner
	// +optional
	Labels []string `json:"labels,omitempty"`

	// MaxActiveRunners is the maximum number of concurrent jobs
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Required
	MaxActiveRunners int `json:"maxActiveRunners"`

	// RegistrationTokenRef references the secret containing the runner registration token
	// +kubebuilder:validation:Required
	RegistrationTokenRef corev1.SecretKeySelector `json:"registrationToken"`

	// AuthTokenRef references the secret containing the Gitea API token for polling
	// +kubebuilder:validation:Required
	AuthTokenRef corev1.SecretKeySelector `json:"authToken"`
}

// RunnerGroupStatus defines the observed state of RunnerGroup.
type RunnerGroupStatus struct {
	// ActiveRunners is the current number of running jobs
	ActiveRunners int `json:"activeRunners"`

	// LastCheckTime is the timestamp of the last poll to Gitea
	// +optional
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// RunnerGroup is the Schema for the runnergroups API.
type RunnerGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RunnerGroupSpec   `json:"spec,omitempty"`
	Status RunnerGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RunnerGroupList contains a list of RunnerGroup.
type RunnerGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RunnerGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RunnerGroup{}, &RunnerGroupList{})
}
