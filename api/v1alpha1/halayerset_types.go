/*
Copyright 2021.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HALayerSetSpec defines the desired state of HALayerSet
type HALayerSetSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//Deployments is a list of deployments that will be managed by the created HA layer.
	Deployments []string `json:"deployments,omitempty"`

	//FenceAgentsSpec list of fence agent to use during HA layer fencing setup.
	FenceAgentsSpec []FenceAgentSpec `json:"fenceAgentsSpec"`

	//NodesSpec contain names and ips of both SNO clusters nodes.
	NodesSpec NodesSpec `json:"nodesSpec"`
}

// FenceAgentSpec contains the necessary information for setting up the fence agent that will be used in the HA layer.
type FenceAgentSpec struct {
	//Name fence agent name.
	Name string `json:"name"`
	//Type of the fence agent.
	Type string `json:"type"`
	//Params parameters which are necessary when creating the fence agent, will be applied in the format: key1=value1 key2=value2 ...
	Params map[string]string `json:"params,omitempty"`
}

// NodesSpec contains names and ips of both SNO clusters nodes.
type NodesSpec struct {
	//FirstNodeName is the name of the first node in the cluster.
	FirstNodeName string `json:"firstNodeName"`
	//SecondNodeName is the name of the second node in the cluster.
	SecondNodeName string `json:"secondNodeName"`
	//FirstNodeIP is the ip address used by the first node in the cluster.
	FirstNodeIP string `json:"firstNodeIP"`
	//SecondNodeIP is the ip address used by the second node in the cluster.
	SecondNodeIP string `json:"secondNodeIP"`
}

// HALayerSetStatus defines the observed state of HALayerSet
type HALayerSetStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// HALayerSet is the Schema for the halayersets API
type HALayerSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HALayerSetSpec   `json:"spec,omitempty"`
	Status HALayerSetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HALayerSetList contains a list of HALayerSet
type HALayerSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HALayerSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HALayerSet{}, &HALayerSetList{})
}
