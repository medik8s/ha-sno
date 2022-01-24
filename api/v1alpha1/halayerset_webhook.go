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
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"os"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	webhookCertDir  = "/apiserver.local.config/certificates"
	webhookCertName = "apiserver.crt"
	webhookKeyName  = "apiserver.key"
)

const (
	nodeNameChangeErrorMsg          = "not allowed to change node name"
	nodeIpChangeErrorMsg            = "not allowed to change node IP"
	containerImageChangeErrorMsg    = "not allowed to change container image"
	duplicateFenceAgentNameErrorMsg = "not allowed to have multiple fence agents with the same name"
)

// log is for logging in this package.
var (
	halayersetlog       = logf.Log.WithName("halayerset-resource")
	setValuePlaceHolder = struct{}{}
)

func (r *HALayerSet) SetupWebhookWithManager(mgr ctrl.Manager) error {

	halayersetlog.Info("start setup with manager")

	certs := []string{filepath.Join(webhookCertDir, webhookCertName), filepath.Join(webhookCertDir, webhookKeyName)}
	certsInjected := true
	for _, fname := range certs {
		if _, err := os.Stat(fname); err != nil {
			certsInjected = false
			break
		}
	}
	if certsInjected {
		server := mgr.GetWebhookServer()
		server.CertDir = webhookCertDir
		server.CertName = webhookCertName
		server.KeyName = webhookKeyName
	} else {
		halayersetlog.Info("OLM injected certs for webhooks not found")
	}

	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-ha-sno-medik8s-io-v1alpha1-halayerset,mutating=false,failurePolicy=fail,sideEffects=None,groups=ha-sno.medik8s.io,resources=halayersets,verbs=create;update,versions=v1alpha1,name=vhalayerset.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &HALayerSet{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *HALayerSet) ValidateCreate() error {
	halayersetlog.Info("validate create", "name", r.Name)
	if err := validateFenceAgentUniqueness(r); err != nil {
		return err
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *HALayerSet) ValidateUpdate(old runtime.Object) error {
	halayersetlog.Info("validate update", "name", r.Name)
	oldCR := old.(*HALayerSet)

	if err := validateFenceAgentUniqueness(r); err != nil {
		return err
	}

	if err := validateNodeSpecs(oldCR, r); err != nil {
		return err
	}

	if err := validateContainerImage(oldCR, r); err != nil {
		return err
	}

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *HALayerSet) ValidateDelete() error {
	halayersetlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

func validateNodeSpecs(oldCR *HALayerSet, r *HALayerSet) error {
	if oldCR.Spec.NodesSpec.FirstNodeName != r.Spec.NodesSpec.FirstNodeName {
		err := fmt.Errorf(nodeNameChangeErrorMsg)
		halayersetlog.Error(err, nodeNameChangeErrorMsg, "original node name", oldCR.Spec.NodesSpec.FirstNodeName, "new node name", r.Spec.NodesSpec.FirstNodeName)
		return err
	} else if oldCR.Spec.NodesSpec.SecondNodeName != r.Spec.NodesSpec.SecondNodeName {
		err := fmt.Errorf(nodeNameChangeErrorMsg)
		halayersetlog.Error(err, nodeNameChangeErrorMsg, "original node name", oldCR.Spec.NodesSpec.SecondNodeName, "new node name", r.Spec.NodesSpec.SecondNodeName)
		return err
	} else if oldCR.Spec.NodesSpec.FirstNodeIP != r.Spec.NodesSpec.FirstNodeIP {
		err := fmt.Errorf(nodeIpChangeErrorMsg)
		halayersetlog.Error(err, nodeIpChangeErrorMsg, "original node IP", oldCR.Spec.NodesSpec.FirstNodeIP, "new node IP", r.Spec.NodesSpec.FirstNodeIP)
		return err
	} else if oldCR.Spec.NodesSpec.SecondNodeIP != r.Spec.NodesSpec.SecondNodeIP {
		err := fmt.Errorf(nodeIpChangeErrorMsg)
		halayersetlog.Error(err, nodeIpChangeErrorMsg, "original node IP", oldCR.Spec.NodesSpec.SecondNodeIP, "new node IP", r.Spec.NodesSpec.SecondNodeIP)
		return err
	}
	return nil
}

func validateFenceAgentUniqueness(r *HALayerSet) error {
	if len(r.Spec.FenceAgentsSpec) < 2 {
		return nil
	}
	fenceAgentsNameSet := make(map[string]struct{})
	for _, spec := range r.Spec.FenceAgentsSpec {
		fenceAgentName := spec.Name
		if _, isAlreadyExist := fenceAgentsNameSet[fenceAgentName]; isAlreadyExist {
			err := fmt.Errorf(duplicateFenceAgentNameErrorMsg)
			halayersetlog.Error(err, "fence agent exist with a duplicate name", "name", fenceAgentName)
			return err
		}
		fenceAgentsNameSet[fenceAgentName] = setValuePlaceHolder
	}
	return nil
}

func validateContainerImage(oldCR *HALayerSet, r *HALayerSet) error {
	if oldCR.Spec.ContainerImage != r.Spec.ContainerImage {
		err := fmt.Errorf(containerImageChangeErrorMsg)
		halayersetlog.Error(err, containerImageChangeErrorMsg, "original container image", oldCR.Spec.ContainerImage, "new container image", r.Spec.ContainerImage)
		return err
	}
	return nil
}
