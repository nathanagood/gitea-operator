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

// GiteaSpec defines the desired state of Gitea
type GiteaSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Ingress for external access
	Ingress IngressSpec `json:"ingress,omitempty"`

	// Object Storage for Gitea
	ObjectStorage *ObjectSpec `json:"objectStorage,omitempty"`

	// Create a loadbalancer for ssh access
	ExternalSSH bool `json:"externalSSH,omitempty"`

	// if different from Hostname
	SSHHostname string `json:"sshHostname,omitempty"`

	// Rootless runs Gitea without root or added capabilities, so on OpenShift
	// the pod runs under the default restricted-v2 SCC with no extra grants.
	// Requires a rootless Gitea image (for example gitea/gitea:1.27.3-rootless).
	// The UID comes from the platform; on plain Kubernetes the data volume must
	// be writable by the image's user (1000).
	// +kubebuilder:default:=false
	Rootless bool `json:"rootless,omitempty"`

	// enable prometheus integrations
	// +kubebuilder:default:=false
	Prometheus bool `json:"prometheus,omitempty"`

	// Additional Prometheus Labels
	PrometheusLabels map[string]string `json:"prometheusLabels,omitempty"`

	// Override the operator set image
	// +kubebuilder:default:="gitea/gitea:1.26.2"
	Image string `json:"image,omitempty"`

	// Use Valkey
	// +kubebuilder:default:=false
	Valkey bool `json:"valkey,omitempty"`

	// Use TLS
	// +kubebuilder:default:=false
	TLS bool `json:"tls,omitempty"`

	// TLS Cert-manager Issuer
	CertIssuer string `json:"certIssuer,omitempty"`

	// Cert-Manger Cluster Issuer Kind
	// +kubebuilder:default:="ClusterIssuer"
	// +kubebuilder:validation:Enum=ClusterIssuer;Issuer
	CertIssuerType string `json:"certIssuerType,omitempty"`

	// ClusterDomain is the cluster's DNS domain, used in the TLS certificate's
	// DNS names. It is detected when unset, and the detected value is not
	// written back here.
	// +optional
	ClusterDomain string `json:"clusterDomain,omitempty"`

	// Postgres Configuration
	Postgres PostgresSpec `json:"postgres,omitempty"`
}

type PostgresSpec struct {
	// Postgres Provider
	// +kubebuilder:default:="zalando"
	// +kubebuilder:validation:Enum=zalando;cnpg
	Provider string `json:"provider,omitempty"`
}

type ObjectSpec struct {
	// Object Storage Type
	// +kubebuilder:default:="minio"
	// +kubebuilder:validation:Enum=minio;gcs;s3
	Type string `json:"type,omitempty"`

	// Object Storage Endpoint
	Endpoint string `json:"endpoint,omitempty"`

	// Object Cloud Provider Region
	Region string `json:"region,omitempty"`
}

type IngressSpec struct {
	// External Hostname of the instance
	// +kubebuilder:example:=git.local
	Host string `json:"host,omitempty"`

	// Ingress Annotations
	Annotations map[string]string `json:"annotations,omitempty"`

	// IngressClassName of the Ingress. Defaults to "nginx", which serves TLS
	// from the Secret "<gitea>-ingress-tls". On OpenShift use
	// "openshift-default": the router turns the Ingress into a Route that
	// serves TLS with the router's default certificate.
	// +optional
	ClassName string `json:"className,omitempty"`
}

// GiteaStatus defines the observed state of Gitea
type GiteaStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	Ready      bool               `json:"ready"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Gitea is the Schema for the gitea API
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Hostname",type="string",priority=1,JSONPath=".spec.hostname"
// +kubebuilder:printcolumn:name="Rootless",type="boolean",priority=1,JSONPath=".spec.rootless"
// +kubebuilder:printcolumn:name="Image",type="string",priority=1,JSONPath=".spec.image"
type Gitea struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GiteaSpec   `json:"spec,omitempty"`
	Status GiteaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GiteaList contains a list of Gitea
type GiteaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Gitea `json:"items"`
}
