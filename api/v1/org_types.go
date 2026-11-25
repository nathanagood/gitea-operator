/*
Copyright 2024.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// OrgSpec defines the desired state of Org
type OrgSpec struct {
	// The organization's name in Gitea, exactly as Gitea shows it (for
	// example "ITZ"). Defaults to metadata.name. Set it when the Gitea name is
	// not a valid Kubernetes name, such as one with capital letters. Only its
	// letter case can be changed once set (Gitea names are case-insensitive).
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.-]+$`
	// +kubebuilder:validation:MaxLength=40
	// +optional
	Name string `json:"name,omitempty"`

	// The human readable name of the organization.
	FullName string `json:"fullname,omitempty"`

	// A brief explanation of the organization.
	Description string `json:"description,omitempty"`

	// The url of the website associated with this org.
	Website string `json:"website,omitempty"`

	// The physical location of the org, if any
	Location string `json:"location,omitempty"`

	// users list to
	Teams []Team `json:"teams,omitempty"`

	// The ACL name of the org. public, private, etc.
	Visibility string `json:"visibility,omitempty"`
	// the gitea instance to build the org in
	Instance InstanceType `json:"instance"`
}

// OrgStatus defines the observed state of Org
type OrgStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Provisioned bool `json:"provisioned"`
}

type Team struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Team-wide access. admin and owner give admin access to every unit and
	// ignore units/unitsMap. read and write apply to every unit in `units`.
	// Unset or none means read, unless unitsMap is set.
	// +kubebuilder:validation:Enum=none;read;write;admin;owner
	Permission      string   `json:"permission,omitempty"`
	CreateOrgRepo   bool     `json:"createOrgRepo,omitempty"`
	IncludeAllRepos bool     `json:"includeAllRepos,omitempty"`
	Members         []string `json:"members,omitempty"`
	// Repository units the team can use (repo.code, repo.issues, ...), all at
	// `permission`. Empty means every unit.
	Units []string `json:"units,omitempty"`
	// Access level per unit, such as {repo.code: write, repo.wiki: read}.
	// Use this for teams whose units have different levels. For read/write
	// teams it replaces `units` and `permission`.
	// +optional
	UnitsMap map[string]TeamUnitAccess `json:"unitsMap,omitempty"`

	// How the operator reconciles Members:
	// Exact removes anyone not listed; Additive only adds listed members and
	// leaves others; Ignore never touches membership.
	// When unset, the team is Ignore if any Auth for the same Gitea instance
	// maps a group to it in groupTeamMap (the identity provider owns its
	// membership), and Exact otherwise.
	// +kubebuilder:validation:Enum=Exact;Additive;Ignore
	// +optional
	MemberPolicy TeamMemberPolicy `json:"memberPolicy,omitempty"`
}

// TeamUnitAccess is a team's access level for one repository unit.
// +kubebuilder:validation:Enum=none;read;write
type TeamUnitAccess string

// TeamMemberPolicy controls how team membership is reconciled.
type TeamMemberPolicy string

const (
	TeamMemberPolicyExact    TeamMemberPolicy = "Exact"
	TeamMemberPolicyAdditive TeamMemberPolicy = "Additive"
	TeamMemberPolicyIgnore   TeamMemberPolicy = "Ignore"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Org is the Schema for the orgs API
// +kubebuilder:validation:XValidation:rule="(has(self.spec) && has(self.spec.name) ? self.spec.name : self.metadata.name).lowerAscii() == (has(oldSelf.spec) && has(oldSelf.spec.name) ? oldSelf.spec.name : oldSelf.metadata.name).lowerAscii()",message="the Gitea organization name (spec.name, or metadata.name when unset) cannot be changed, except for its letter case"
// +kubebuilder:printcolumn:name="Gitea Name",type="string",JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=`.status.provisioned`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Org struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OrgSpec   `json:"spec,omitempty"`
	Status OrgStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OrgList contains a list of Org
type OrgList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Org `json:"items"`
}

// GiteaName returns the organization's name in Gitea: spec.name, or
// metadata.name when that is unset.
func (o *Org) GiteaName() string {
	if o.Spec.Name != "" {
		return o.Spec.Name
	}
	return o.Name
}
