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

package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	appv1alpha1 "github.com/mshitrit/hasno-setup-operator/api/v1alpha1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"net/http"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strings"
	"time"
)

//+kubebuilder:rbac:groups=app.hasno.com,resources=halayersets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=app.hasno.com,resources=halayersets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=app.hasno.com,resources=halayersets/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;create;delete;watch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;delete;create;watch
//+kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
//+kubebuilder:rbac:groups=core,resources=services,verbs=create;delete;update;watch;list

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=create
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=create

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the HALayerSet object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile

const (
	//Assumptions
	//1. Cluster domain is shared by both clusters
	//2. SNO1 & SNO2 are named cluster1 & cluster2
	haClusterName      = "aio-pair"
	sno1Name           = "cluster1"
	sno2Name           = "cluster2"
	serviceName        = "cluster"
	serviceAccountName = "hasno-setup-operator-aio-cluster-role"

	deploymentNamespaceEnvVar = "DEPLOYMENT_NAMESPACE"
	haSnoFinalizer            = "app.hasno.com/finalizer"
)

var (
	haPodLabels = map[string]string{"app": haClusterName}
)

type Scenario int

const (
	initialize Scenario = iota
	update
	deletion
	noAction
	failure
)

// HALayerSetReconciler reconciles a HALayerSet object
type HALayerSetReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	pacemakerCommandHandler
}

func (r *HALayerSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("halayerset", req.NamespacedName)
	log.Info("reconciling...")
	r.initPcsCommandHandler()
	var err error

	if isProperNamespace, err := r.verifyCRNamespace(req.Namespace); err != nil {
		return ctrl.Result{}, err
	} else if !isProperNamespace {
		return ctrl.Result{}, nil
	}

	var hals *appv1alpha1.HALayerSet
	hals, err = r.getHals(ctx, req)
	if err != nil || hals == nil {
		return ctrl.Result{}, err
	}

	var scenario Scenario
	if scenario, err = r.getScenario(hals); err != nil {
		return ctrl.Result{}, err
	}

	switch scenario {
	case initialize:
		log.Info("initialize new HALayer pod")
		return r.initialize(ctx, req)
	case update:
		log.Info("updating a HALayer pod")
		return r.update(ctx, req)
	case deletion:
		log.Info("deleting a HALayer pod")
		return r.deletion(ctx, req)
	case noAction:
		log.Info("reconciling triggered, but no change is needed")
		return ctrl.Result{}, nil
	case failure:
		return ctrl.Result{}, err
	default:
		errMsg := "unknown reconcile scenario"
		r.Log.Error(fmt.Errorf(errMsg), errMsg)
		return ctrl.Result{}, fmt.Errorf(errMsg)
	}

}

func (r *HALayerSetReconciler) getHals(ctx context.Context, req ctrl.Request) (*appv1alpha1.HALayerSet, error) {
	hals := new(appv1alpha1.HALayerSet)
	key := client.ObjectKey{Name: req.Name, Namespace: req.Namespace}
	err := r.Client.Get(ctx, key, hals)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		r.Log.Error(err, "failed fetching HA layer CR", "name", req.Name, "namespace", req.Namespace)
		return nil, err
	}
	return hals, nil
}

func (r *HALayerSetReconciler) initialize(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	hals, err := r.getHALayerSet(ctx, req)
	if err != nil {
		return ctrl.Result{}, err
	}
	//set var for ha layer pod name
	haLayerPodTemplateName = createPodTemplateNameFromCRName(hals.Name)

	if err := r.addFinalizer(ctx, hals); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.createHALayer(hals); err != nil {
		return ctrl.Result{}, err
	}

	nodeName, err := r.getNodeName()
	if err != nil {
		return ctrl.Result{}, err
	}

	if r.isMainNode(hals, nodeName) {
		if err := r.verifyPodIsRunning(hals); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.createFenceAgents(hals); err != nil {
			return ctrl.Result{}, err
		}

		if err = r.createDeployments(hals.Spec.Deployments, hals.Namespace); err != nil {
			return ctrl.Result{}, err
		}
	}
	r.Log.Info("initialize finished successfully")
	return ctrl.Result{}, nil
}

func createPodTemplateNameFromCRName(halsCRName string) string {
	return fmt.Sprintf("%s-%s", halsCRName, podNameHALayerSuffix)
}

func (r *HALayerSetReconciler) addFinalizer(ctx context.Context, hals *appv1alpha1.HALayerSet) error {
	if !controllerutil.ContainsFinalizer(hals, haSnoFinalizer) {
		controllerutil.AddFinalizer(hals, haSnoFinalizer)
		if err := r.Update(ctx, hals); err != nil {
			r.Log.Error(err, "failed to add finalizer on HALayerSet resource")
			return err
		}
	}
	return nil
}

func (r *HALayerSetReconciler) deletion(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	hals, err := r.getHALayerSet(ctx, req)
	if err != nil {
		return ctrl.Result{}, err
	}

	var isAllowedToModify bool
	if isAllowedToModify, err = r.isAllowedToModify(hals); err != nil {
		return ctrl.Result{}, err
	}
	if isAllowedToModify {
		//Delete pcs resources in HA Layer
		if err := r.deleteHALayerResources(hals.Namespace); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.deleteHALayer(hals.Namespace); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.removeFinalizer(hals); err != nil {
		return ctrl.Result{}, err
	}
	r.Log.Info("delete finished successfully")
	return ctrl.Result{}, nil
}

func (r *HALayerSetReconciler) removeFinalizer(hals *appv1alpha1.HALayerSet) error {
	controllerutil.RemoveFinalizer(hals, haSnoFinalizer)
	if err := r.Update(context.Background(), hals); err != nil {
		r.Log.Error(err, "failed to remove finalizer from HALayerSet CR")
		return err
	}
	return nil
}

func (r *HALayerSetReconciler) deleteHALayerResources(namespace string) error {
	resources, err := r.getHALayerResources(namespace)
	if err != nil {
		return err
	}
	for _, resource := range resources.Resources {
		if err := r.removeResource(resource.Id, namespace); err != nil {
			return err
		}
	}
	return nil
}

func (r *HALayerSetReconciler) deleteHADeployment(namespace string) error {
	haDeployment, err := r.getHADeployment(namespace)
	if err != nil {
		return err
	}

	if err := r.Client.Delete(context.Background(), haDeployment); err != nil {
		r.Log.Error(err, "Can't delete high availability deployment as part of tearing down the high availability layer")
		return err
	}

	r.pacemakerCommandHandler.postDeploymentDeleteHook()
	return nil
}

func (r *HALayerSetReconciler) getHADeployment(namespace string) (*v1.Deployment, error) {
	haDeployment := &v1.Deployment{}
	key := client.ObjectKey{Name: r.getHADeploymentName(), Namespace: namespace}
	if err := r.Client.Get(context.Background(), key, haDeployment); err != nil {
		r.Log.Error(err, "Can't fetch high availability deployment as part of tearing down the high availability layer")
		return nil, err
	}
	return haDeployment, nil
}

func (r *HALayerSetReconciler) createHADeployment(hals *appv1alpha1.HALayerSet) error {
	nodeName, err := r.getNodeName()
	if err != nil {
		return err
	}
	pod := r.buildHALayerPod(hals, nodeName)
	var replicas int32 = 1
	deployment := &v1.Deployment{}
	deployment.Spec.Replicas = &replicas
	deployment.Spec.Template.Spec = pod.Spec
	deployment.Spec.Template.Name = haLayerPodTemplateName
	deployment.Spec.Template.Namespace = pod.Namespace
	deployment.Namespace = pod.Namespace
	deployment.Name = r.getHADeploymentName()
	//Labels
	deployment.Labels = pod.Labels
	deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: pod.Labels}
	deployment.Spec.Template.Labels = pod.Labels

	if err := r.Client.Create(context.Background(), deployment); err != nil {
		if !errors.IsAlreadyExists(err) {
			r.Log.Error(err, "Can't create HALayer deployment as part of setting up the high availability layer")
			return err
		}
		r.Log.Info("HALayer deployment already exist", "deployment name", deployment.Name)
	} else {
		r.Log.Info("HALayer deployment created", "deployment name", deployment.Name)
	}
	r.pacemakerCommandHandler.postDeploymentCreateHook()
	return nil
}

func (r *HALayerSetReconciler) getHADeploymentName() string {
	return fmt.Sprintf("%s-deployment", haLayerPodTemplateName)
}

func (r *HALayerSetReconciler) buildHALayerPod(hals *appv1alpha1.HALayerSet, nodeName string) *corev1.Pod {
	pod := &corev1.Pod{}
	pod.Namespace = hals.Namespace
	pod.Labels = haPodLabels
	pod.Spec.Hostname = nodeName
	pod.Spec.ServiceAccountName = serviceAccountName

	var hostPathTypeVal = corev1.HostPathDirectoryOrCreate
	pod.Spec.Volumes = []corev1.Volume{
		{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "secret-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "cluster-secrets"}}},
		{Name: "cluster-state", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/pcmkdata", Type: &hostPathTypeVal}}},
	}

	pod.Spec.HostNetwork = true
	pod.Spec.HostAliases = []corev1.HostAlias{
		{IP: hals.Spec.NodesSpec.FirstNodeIP, Hostnames: []string{sno1Name}},
		{IP: hals.Spec.NodesSpec.SecondNodeIP, Hostnames: []string{sno2Name}},
	}
	trueVal := true
	pod.Spec.Containers = []corev1.Container{
		{Name: "system", Image: "quay.io/mshitrit/pcmk:v3", ImagePullPolicy: corev1.PullAlways, Command: []string{"/usr/lib/systemd/systemd", "--system"},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "cluster-state", MountPath: "/var/lib/pcsd"},
				{Name: "cluster-state", MountPath: "/etc/corosync"},
				{Name: "cluster-state", MountPath: "/var/lib/pacemaker"},
			},
			SecurityContext: &corev1.SecurityContext{Privileged: &trueVal},
		},

		{Name: "corosync", Image: "quay.io/mshitrit/pcmk:v3", ImagePullPolicy: corev1.PullAlways, Command: []string{"/usr/sbin/corosync", "-f"},
			VolumeMounts:    []corev1.VolumeMount{{Name: "cluster-state", MountPath: "/etc/corosync"}},
			SecurityContext: &corev1.SecurityContext{Privileged: &trueVal},
		},

		{Name: "pacemaker", Image: "quay.io/mshitrit/pcmk:v3", ImagePullPolicy: corev1.PullAlways, Command: []string{"/usr/sbin/pacemakerd", "-f"},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "cluster-state", MountPath: "/etc/pacemaker"},
				{Name: "cluster-state", MountPath: "/var/lib/pacemaker"},
			},
			SecurityContext: &corev1.SecurityContext{Privileged: &trueVal},
		},
	}

	pod.Spec.InitContainers = []corev1.Container{
		{Name: "init", Image: "quay.io/mshitrit/pcmk:v3", ImagePullPolicy: corev1.PullAlways, Command: []string{"/root/setup.sh"},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "cluster-state", MountPath: "/etc/corosync"},
				{Name: "cluster-state", MountPath: "/etc/pacemaker"},
				{Name: "cluster-state", MountPath: "/var/lib/pacemaker"},
				{Name: "cluster-state", MountPath: "/var/lib/pcsd"},
				{Name: "secret-volume", ReadOnly: true, MountPath: "/etc/secret-volume"},
			},
			Env: []corev1.EnvVar{
				//this values are necessary for corosync container creation.
				//source code at: https://github.com/mshitrit/pcmk/blob/add_fence_agents/setup.sh
				{Name: "NODE1NAME", Value: sno1Name},
				{Name: "NODE1ADDR", Value: hals.Spec.NodesSpec.FirstNodeIP},
				{Name: "NODE2NAME", Value: sno2Name},
				{Name: "NODE2ADDR", Value: hals.Spec.NodesSpec.SecondNodeIP},

				{Name: "CLUSTER_NAME", Value: haClusterName},
				{Name: "SECRETS_DIR", Value: "/etc/secret-volume"},
			},
			SecurityContext: &corev1.SecurityContext{Privileged: &trueVal},
		},
	}
	return pod
}

func (r *HALayerSetReconciler) deleteHAService(namespace string) error {
	service := &corev1.Service{}
	service.Namespace = namespace
	service.Name = serviceName
	if err := r.Client.Delete(context.Background(), service); err != nil {
		r.Log.Error(err, "Can't delete Service as part of tearing down the high availability layer")
		return err
	}
	return nil
}

func (r *HALayerSetReconciler) createHAService(hals *appv1alpha1.HALayerSet) error {

	service := &corev1.Service{}
	r.setUpServiceData(hals, service)

	if err := r.Client.Create(context.Background(), service); err != nil {
		if errors.IsAlreadyExists(err) {
			r.Log.Info("Setting up high availability layer, service already exist")
			key := client.ObjectKey{Name: service.Name, Namespace: hals.Namespace}
			if err := r.Client.Get(context.Background(), key, service); err == nil {

				r.setUpServiceData(hals, service)
				if err := r.Client.Update(context.Background(), service); err != nil {
					r.Log.Error(err, "Can't create Service as part of setting up the high availability layer, service already exit and update failed")
					return err
				}
			} else {
				r.Log.Error(err, "Can't create Service as part of setting up the high availability layer, failed to fetch the service in order to update an existing service")
			}

		} else {
			r.Log.Error(err, "Can't create Service as part of setting up the high availability layer")
			return err
		}

	}
	r.Log.Info("Setting up high availability layer, service created", "service name", service.Name)
	return nil
}

func (r *HALayerSetReconciler) setUpServiceData(hals *appv1alpha1.HALayerSet, service *corev1.Service) {
	service.Namespace = hals.Namespace

	service.Name = serviceName
	service.Spec.Selector = map[string]string{"app": haClusterName}
	service.Spec.Type = corev1.ServiceTypeClusterIP

	ports := []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 2224, Name: "pcs"}}
	for i := 1; i < 10; i++ {
		ports = append(ports, corev1.ServicePort{Protocol: corev1.ProtocolUDP, Port: int32(5403 + i), Name: fmt.Sprintf("corosync%v", i)})
	}
	service.Spec.Ports = ports
}

func (r *HALayerSetReconciler) createHALayer(hals *appv1alpha1.HALayerSet) error {
	if err := r.createHAService(hals); err != nil {
		return err
	}
	if err := r.createHADeployment(hals); err != nil {
		return err
	}
	return nil
}

func (r *HALayerSetReconciler) deleteHALayer(namespace string) error {
	if err := r.deleteHAService(namespace); err != nil {
		return err
	}

	if err := r.deleteHADeployment(namespace); err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HALayerSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1alpha1.HALayerSet{}).
		Complete(r)
}

func (r *HALayerSetReconciler) verifyDeployment(deploymentName string, namespace string) error {
	deployment := new(v1.Deployment)
	key := client.ObjectKey{Name: deploymentName, Namespace: namespace}
	var err error
	if err = r.Client.Get(context.Background(), key, deployment); err != nil {
		r.Log.Error(err, "Error fetching deployment", "deployment name", deploymentName)
		return err
	}
	//new deployment are created with 0 replicas, upon creation of resources in  the pacemaker container (in HALayer pod) the replicas will be adjusted.
	if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas > 0 {
		r.Log.Error(err, "Managed deployment replicas should be 0 ", "deployment name", deploymentName, "replicas", *deployment.Spec.Replicas)
		return err
	}
	return nil
}

func (r *HALayerSetReconciler) getHAPod(namespace string) (*corev1.Pod, error) {

	pods := new(corev1.PodList)

	haPodLabelsSelector, _ := metav1.LabelSelectorAsSelector(
		&metav1.LabelSelector{MatchLabels: haPodLabels})
	options := client.ListOptions{
		LabelSelector: haPodLabelsSelector,
		Namespace:     namespace,
	}
	if err := r.Client.List(context.Background(), pods, &options); err != nil {
		r.Log.Error(err, "failed fetching HA layer pod")
		return nil, err
	}
	if len(pods.Items) == 0 {
		r.Log.Info("No HA Layer pods were found")
		podNotFoundErr := &errors.StatusError{ErrStatus: metav1.Status{
			Status: metav1.StatusFailure,
			Code:   http.StatusNotFound,
			Reason: metav1.StatusReasonNotFound,
		}}
		return nil, podNotFoundErr
	}
	return &pods.Items[0], nil

}

func (r *HALayerSetReconciler) verifyPodIsRunning(hals *appv1alpha1.HALayerSet) error {
	var pod *corev1.Pod
	var err error
	var podPhase corev1.PodPhase
	waitTimeLeftSeconds := 30
	for waitTimeLeftSeconds > 0 {
		if pod, err = r.getHAPod(hals.Namespace); err != nil {
			time.Sleep(time.Second)
			waitTimeLeftSeconds--
		} else {
			err = nil
			podPhase = pod.Status.Phase

			if r.isPodRunning(podPhase) {
				return nil
			}
			time.Sleep(time.Second)
			waitTimeLeftSeconds--
		}
	}

	if err != nil {
		r.Log.Error(err, "Could not retrieve HA pod", "pod template name", haLayerPodTemplateName)
	} else {
		err = fmt.Errorf("ha layer pod didn't start properly")
		r.Log.Error(err, "HA pod didn't start properly", "pod template name", haLayerPodTemplateName, "pod phase", podPhase)
	}
	return err
}

func (r *HALayerSetReconciler) getNodeName() (string, error) {
	podList := new(corev1.PodList)
	var err error
	//since SNO has a single node, all the pods should belong to it so no filtering is required
	if err = r.Client.List(context.Background(), podList); err == nil && len(podList.Items) == 0 {
		err = fmt.Errorf("no pods were found")
	}
	if err != nil {
		r.Log.Error(err, "Failed to fetch cluster name")
		return "", err
	}
	return podList.Items[0].Spec.NodeName, nil
}

func (r *HALayerSetReconciler) getScenario(hals *appv1alpha1.HALayerSet) (Scenario, error) {
	if _, err := r.getHAPod(hals.Namespace); err == nil { //Pod exist and CR exist - update scenario
		isHalsMarkedToBeDeleted := hals.GetDeletionTimestamp() != nil
		if isHalsMarkedToBeDeleted {
			return deletion, nil
		} else {
			return update, nil
		}
	} else if errors.IsNotFound(err) { //Pod does not exist but CR does - init scenario
		return initialize, nil
	} else { //error fetching Pod
		return failure, err
	}
}

func (r *HALayerSetReconciler) update(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	//Get the resource
	hals, err := r.getHALayerSet(ctx, req)
	if err != nil {
		return ctrl.Result{}, err
	}

	var isAllowedToModify bool
	if isAllowedToModify, err = r.isAllowedToModify(hals); err != nil {
		return ctrl.Result{}, err
	}
	if isAllowedToModify {
		if err := r.verifyPodIsRunning(hals); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.reconcileFenceAgents(hals); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.reconcileDeployments(hals); err != nil {
			return ctrl.Result{}, err
		}
	}
	r.Log.Info("update finished successfully")
	return ctrl.Result{}, nil
}

func (r *HALayerSetReconciler) getHALayerSet(ctx context.Context, req ctrl.Request) (*appv1alpha1.HALayerSet, error) {
	hals := new(appv1alpha1.HALayerSet)
	key := client.ObjectKey{Name: req.Name, Namespace: req.Namespace}
	if err := r.Client.Get(ctx, key, hals); err != nil {
		r.Log.Error(err, "failed to fetch HALayerSet CR", "HALayerSet Name", req.Name, "Namespace", req.Namespace)
		return nil, err
	}
	return hals, nil
}

func (r *HALayerSetReconciler) reconcileFenceAgents(hals *appv1alpha1.HALayerSet) error {

	actualFenceAgents, err := r.getFenceAgentNames(hals.Namespace)
	if err != nil {
		return err
	}

	//this map is used as a set
	expectedFenceAgent := map[string]struct{}{}
	placeHolder := struct{}{}
	for _, fenceAgent := range hals.Spec.FenceAgentsSpec {
		expectedFenceAgent[fenceAgent.Name] = placeHolder
	}

	deleteCandidates := r.getElementsOnlyInSecondMap(expectedFenceAgent, actualFenceAgents)
	newCreateCandidates := r.getElementsOnlyInSecondMap(actualFenceAgents, expectedFenceAgent)

	if len(newCreateCandidates) == 0 {
		r.Log.Info("no fence agents to create")
	}

	if len(deleteCandidates) == 0 {
		r.Log.Info("no fence agents to delete")
	}

	for _, fenceAgentToDelete := range deleteCandidates {
		if err := r.removeResource(fenceAgentToDelete, hals.Namespace); err != nil {
			return err
		}
	}

	for _, fenceAgentNameToCreate := range newCreateCandidates {
		if err := r.createFenceAgent(r.getFenceAgentByName(fenceAgentNameToCreate, hals), hals.Namespace); err != nil {
			return err
		}
	}

	return nil
}

func (r *HALayerSetReconciler) getFenceAgentByName(name string, hals *appv1alpha1.HALayerSet) appv1alpha1.FenceAgentSpec {

	var fenceAgentToCreate appv1alpha1.FenceAgentSpec
	for _, fenceAgent := range hals.Spec.FenceAgentsSpec {
		if fenceAgent.Name == name {
			fenceAgentToCreate = fenceAgent
			break
		}
	}
	return fenceAgentToCreate
}

func (r *HALayerSetReconciler) reconcileDeployments(hals *appv1alpha1.HALayerSet) error {
	actualDeployments, expectedDeployments := map[string]struct{}{}, map[string]struct{}{}
	resources, err := r.getHALayerResources(hals.Namespace)
	if err != nil {
		return err
	}
	for _, resource := range resources.Resources {
		if r.isDeploymentResource(resource) {
			actualDeployments[strings.TrimPrefix(resource.Id, deploymentResourcePrefix)] = struct{}{}
		}
	}
	for _, dep := range hals.Spec.Deployments {
		expectedDeployments[dep] = struct{}{}
	}
	createCandidateDeployments, deleteCandidateDeployments := r.getElementsOnlyInSecondMap(actualDeployments, expectedDeployments), r.getElementsOnlyInSecondMap(expectedDeployments, actualDeployments)
	if err := r.createDeployments(createCandidateDeployments, hals.Namespace); err != nil {
		return err
	}
	if err := r.deleteDeployments(deleteCandidateDeployments, hals.Namespace); err != nil {
		return err
	}
	return nil
}

func (r *HALayerSetReconciler) getElementsOnlyInSecondMap(firstMap map[string]struct{}, secondMap map[string]struct{}) []string {
	var elementOnlyInSecondMap []string

	for secondMapElement := range secondMap {
		if _, isExist := firstMap[secondMapElement]; !isExist {
			elementOnlyInSecondMap = append(elementOnlyInSecondMap, secondMapElement)
		}
	}
	return elementOnlyInSecondMap
}

func (r *HALayerSetReconciler) verifyCRNamespace(actualNamespace string) (bool, error) {
	if expectedDeploymentNamespace, isFound := os.LookupEnv(deploymentNamespaceEnvVar); !isFound {
		err := fmt.Errorf("envrionment variable %s was not found", deploymentNamespaceEnvVar)
		r.Log.Error(err, "could not find deployment namespace")
		return false, err
	} else {
		if expectedDeploymentNamespace != actualNamespace {
			r.Log.Info("CR was not created in the operator deployment namespace, no changes apply.", "cr's namespace", actualNamespace, "deployment's namespace", expectedDeploymentNamespace)
			return false, nil
		} else {
			return true, nil
		}
	}
}

func (r *HALayerSetReconciler) initPcsCommandHandler() {
	if r.pacemakerCommandHandler == nil {
		r.pacemakerCommandHandler = &basePacemakerCommandHandler{r.Log}

	}
}

func (r *HALayerSetReconciler) createFenceAgent(agent appv1alpha1.FenceAgentSpec, namespace string) error {
	command := []string{"pcs", "stonith", "create", agent.Name, agent.Type}
	//append fence agent params to command
	for key, val := range agent.Params {
		command = append(command, fmt.Sprintf("%s=%s", key, val))
	}

	if pod, err := r.getHAPod(namespace); err == nil {
		_, _, err := r.execCmdOnPacemaker(command, pod)
		if err != nil {
			r.Log.Error(err, "Can't create fence agent on HA Layer pod", "HA Layer pod template name", haLayerPodTemplateName, "fence agent name", agent.Name, "fence agent type", agent.Type)
			return err
		}
		r.Log.Info("fence agent created", "fence agent name", agent.Name, "fence agent type", agent.Type)
	} else {
		return err
	}
	return nil
}
