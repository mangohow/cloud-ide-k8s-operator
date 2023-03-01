/*
Copyright 2023.

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

type WorkSpaceOperation string

const (
	WorkSpaceStart WorkSpaceOperation = "Start"
	WorkSpaceStop                     = "Stop"
)

type WorkSpacePhase string

const (
	WorkspacePhaseRunning WorkSpacePhase = "Running"
	WorkspacePhaseStopped                = "Stopped"
)

// WorkSpaceSpec defines the desired state of WorkSpace
type WorkSpaceSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Workspace machine specifications
	Cpu     string `json:"cpu,omitempty"`
	Memory  string `json:"memory,omitempty"`
	Storage string `json:"storage,omitempty"`

	// 硬件资源
	Hardware string `json:"hardware,omitempty"`

	// The image
	Image string `json:"image,omitempty"`

	// Exposed port
	Port int32 `json:"port,omitempty"`

	// Volume mount path
	MountPath string `json:"mountPath"`

	// The operation to do
	Operation WorkSpaceOperation `json:"operation,omitempty"`
}

// WorkSpaceStatus defines the observed state of WorkSpace
type WorkSpaceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase WorkSpacePhase `json:"phase,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Hardware",type=string,JSONPath=`.spec.hardware`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// WorkSpace is the Schema for the workspaces API
type WorkSpace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkSpaceSpec   `json:"spec,omitempty"`
	Status WorkSpaceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WorkSpaceList contains a list of WorkSpace
type WorkSpaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkSpace `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkSpace{}, &WorkSpaceList{})
}
