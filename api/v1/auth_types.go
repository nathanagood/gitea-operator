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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AuthDeletionPolicy controls what happens to the Gitea authentication
// source when the Auth resource is deleted.
// +kubebuilder:validation:Enum=Retain;Delete
type AuthDeletionPolicy string

const (
	// AuthDeletionRetain leaves the authentication source in Gitea.
	AuthDeletionRetain AuthDeletionPolicy = "Retain"
	// AuthDeletionDelete removes the authentication source from Gitea.
	// Gitea refuses to delete a source that still has users bound to it.
	AuthDeletionDelete AuthDeletionPolicy = "Delete"
)

// GroupTeamMap maps a group claim value to the organization teams its
// members are synchronized into: {group: {orgName: [teamName, ...]}}.
// It is serialized to Gitea's "Map claimed groups to Organization teams" JSON.
type GroupTeamMap map[string]map[string][]string

// AuthCustomURLs overrides the OAuth2 endpoints for providers that support it
// (GitHub, GitLab, Gitea, Nextcloud, Azure AD, ...). Not used by openidConnect.
type AuthCustomURLs struct {
	AuthURL    string `json:"authURL,omitempty"`
	TokenURL   string `json:"tokenURL,omitempty"`
	ProfileURL string `json:"profileURL,omitempty"`
	EmailURL   string `json:"emailURL,omitempty"`
	TenantID   string `json:"tenantID,omitempty"`
}

// AuthSpec defines the desired state of an OAuth2 authentication source.
// Every field maps to a flag of `gitea admin auth add-oauth`.
type AuthSpec struct {
	// The authentication source name shown in Gitea (e.g. "IBM Security Verify").
	// Used to find an existing source. Defaults to metadata.name.
	// +optional
	Name string `json:"name,omitempty"`

	// The OAuth2 provider, e.g. openidConnect, github, gitlab, azureadv2.
	// +kubebuilder:validation:Required
	Provider string `json:"provider"`

	// The client ID for the OAuth2 provider
	// +kubebuilder:validation:Required
	ClientID corev1.SecretKeySelector `json:"clientID"`
	// The client secret for the OAuth2 provider
	// +kubebuilder:validation:Required
	ClientSecret corev1.SecretKeySelector `json:"clientSecret"`

	// The OpenID Connect discovery document URL
	// (e.g. https://example.verify.ibm.com/oidc/endpoint/default/.well-known/openid-configuration).
	// Required when provider is openidConnect.
	// +optional
	AutoDiscoveryURL string `json:"autoDiscoveryURL,omitempty"`

	// Custom endpoint URLs for providers that support them.
	// +optional
	CustomURLs *AuthCustomURLs `json:"customURLs,omitempty"`

	// Custom icon URL shown on the sign-in button.
	// +optional
	IconURL string `json:"iconURL,omitempty"`

	// Additional scopes to request from the provider.
	// +optional
	Scopes []string `json:"scopes,omitempty"`

	// Skip local two-factor authentication for users of this source.
	// +optional
	SkipLocal2FA bool `json:"skipLocal2FA,omitempty"`

	// Claim that must be present for a user to sign in with this source.
	// +optional
	RequiredClaimName string `json:"requiredClaimName,omitempty"`
	// Value the required claim must have (any value when empty).
	// +optional
	RequiredClaimValue string `json:"requiredClaimValue,omitempty"`

	// Claim that provides the user's group names (e.g. blue_groups).
	// +optional
	GroupClaimName string `json:"groupClaimName,omitempty"`
	// Group whose members are made Gitea site administrators.
	// +optional
	AdminGroup string `json:"adminGroup,omitempty"`
	// Group whose members are made restricted users.
	// +optional
	RestrictedGroup string `json:"restrictedGroup,omitempty"`

	// Maps group claim values to organization teams. On sign-in Gitea adds
	// the user to the teams mapped from each of their groups.
	// +optional
	GroupTeamMap GroupTeamMap `json:"groupTeamMap,omitempty"`
	// Remove users from mapped teams when they no longer have the group.
	// +optional
	GroupTeamMapRemoval bool `json:"groupTeamMapRemoval,omitempty"`

	// Claim that provides the user's SSH public keys.
	// +optional
	SSHPublicKeyClaimName string `json:"sshPublicKeyClaimName,omitempty"`
	// Claim that provides the user's full name.
	// +optional
	FullNameClaimName string `json:"fullNameClaimName,omitempty"`

	// What to do with the Gitea source when this resource is deleted.
	// +kubebuilder:default=Retain
	// +optional
	DeletionPolicy AuthDeletionPolicy `json:"deletionPolicy,omitempty"`

	// The Gitea instance to add the authentication source to
	// +kubebuilder:validation:Required
	Instance InstanceType `json:"instance"`
}

// AuthStatus defines the observed state of Auth
type AuthStatus struct {
	// The Gitea authentication source ID (what User.spec.sourceId refers to).
	// +optional
	SourceID int64 `json:"sourceId,omitempty"`

	// Hash of the last configuration (including secret values) applied to Gitea.
	// +optional
	AppliedHash string `json:"appliedHash,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="ID",type="integer",JSONPath=`.status.sourceId`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Auth is the Schema for the auths API
type Auth struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuthSpec   `json:"spec,omitempty"`
	Status AuthStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AuthList contains a list of Auth
type AuthList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Auth `json:"items"`
}
