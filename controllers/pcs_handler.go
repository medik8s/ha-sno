package controllers

import (
	"encoding/xml"
	"fmt"
	appv1alpha1 "github.com/mshitrit/hasno-setup-operator/api/v1alpha1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"strings"
	"time"
)

const (
	containerNamePacemaker   = "pacemaker"
	pcsDeploymentIdentifier  = "k8sDeployment"
	pcsFenceIdentifier       = "stonith"
	fenceAgentPrefix         = "fencing_agent_"
	deploymentResourcePrefix = "pcs_deployment_resource_"
	podNameHALayerSuffix     = "ha-layer-pod"
)

var (
	kClient                *kubernetes.Clientset
	k8sClientConfig        *restclient.Config
	haLayerPodTemplateName string
)

type haStatus struct {
	XMLName   xml.Name  `xml:"crm_mon"`
	Nodes     nodes     `xml:"nodes"`
	Resources resources `xml:"resources"`
}

type resource struct {
	XMLName       xml.Name `xml:"resource"`
	Id            string   `xml:"id,attr"`
	ResourceAgent string   `xml:"resource_agent,attr"`
}

type resources struct {
	XMLName   xml.Name   `xml:"resources"`
	Resources []resource `xml:"resource"`
}

type node struct {
	XMLName xml.Name `xml:"node"`
	Name    string   `xml:"name,attr"`
	Id      string   `xml:"id,attr"`
	Online  string   `xml:"online,attr"`
}

func (n *node) isOnline() bool {
	return n.Online == "true"
}

type nodes struct {
	XMLName xml.Name `xml:"nodes"`
	Nodes   []node   `xml:"node"`
}

func (r *HALayerSetReconciler) isDeploymentResource(res resource) bool {
	split := strings.Split(res.ResourceAgent, ":")
	return split[len(split)-1] == pcsDeploymentIdentifier
}

func (r *HALayerSetReconciler) isFenceResource(res resource) bool {
	split := strings.Split(res.ResourceAgent, ":")
	return split[0] == pcsFenceIdentifier
}

func (r *HALayerSetReconciler) getHALayerResources(namespace string) (*resources, error) {
	if status, err := r.getHALayerStatus(namespace); err != nil {
		return nil, err
	} else {
		return &status.Resources, nil
	}
}

func (r *HALayerSetReconciler) isAllowedToModify(hals *appv1alpha1.HALayerSet) (bool, error) {
	nodeName, err := r.getNodeName()
	if err != nil {
		return false, err
	}

	isMainNode := r.isMainNode(hals, nodeName)
	if isMainNode {
		return true, nil
	}

	//necessary for next step of querying peer
	if err := r.verifyPodIsRunning(hals); err != nil {
		return false, err
	}

	isPeerActive, err := r.verifyIsPeerActive(hals.Namespace)
	if err != nil {
		return false, err
	}
	if isPeerActive {
		r.Log.Info("CR change will not be applied by the operator because current node does not have priority to apply the modification", "current node name", nodeName, "all nodes names", fmt.Sprintf("%s, %s", hals.Spec.NodesSpec.FirstNodeName, hals.Spec.NodesSpec.SecondNodeName))
	}
	return !isPeerActive, nil

}

func (r *HALayerSetReconciler) verifyIsPeerActive(namespace string) (bool, error) {
	var isPeerActive bool
	var err error
	for i := 0; i < 30; i++ {
		if isPeerActive, err = r.isPeerActive(namespace); err == nil {
			return isPeerActive, nil
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		r.Log.Error(err, "unexpected online nodes status")
	}
	return isPeerActive, err

}
func (r *HALayerSetReconciler) isPeerActive(namespace string) (bool, error) {
	if status, err := r.getHALayerStatus(namespace); err != nil {
		return false, err
	} else {
		haNodes := status.Nodes.Nodes
		if len(haNodes) == 1 {
			return false, nil
		}
		if len(haNodes) != 2 {
			return false, fmt.Errorf("expecting max 1 or 2 nodes, actual number of nodes:%v", len(haNodes))
		}
		if !haNodes[0].isOnline() && !haNodes[1].isOnline() {

			return false, fmt.Errorf("expecting at least one node online, node1 Name:%s, node1 online status:%s, node2 Name:%s, node2 online status:%s", haNodes[0].Name, haNodes[0].Online, haNodes[1].Name, haNodes[1].Online)
		}
		return haNodes[0].isOnline() && haNodes[1].isOnline(), nil
	}
}
func (r *HALayerSetReconciler) isMainNode(hals *appv1alpha1.HALayerSet, nodeName string) bool {
	isFirstNode := nodeName <= hals.Spec.NodesSpec.FirstNodeName && nodeName <= hals.Spec.NodesSpec.SecondNodeName
	return isFirstNode
}

func (r *HALayerSetReconciler) getHALayerStatus(namespace string) (*haStatus, error) {
	pod, err := r.getHAPod(namespace)
	if err != nil {
		return nil, err
	}

	if result, _, err := r.execCmdOnPacemaker([]string{"pcs", "status", "xml"}, pod); err != nil {
		r.Log.Error(err, "failed to retrieve the status of the HA layer")
		return nil, err
	} else {
		return r.pacemakerCommandHandler.extractHAStatus(result)
	}
}

func (r *HALayerSetReconciler) getFenceAgentNames(namespace string) (map[string]struct{}, error) {
	fenceAgents := map[string]struct{}{}
	resources, err := r.getHALayerResources(namespace)
	if err != nil {
		return fenceAgents, err
	}
	placeHolder := struct{}{}
	for _, resource := range resources.Resources {
		if r.isFenceResource(resource) {
			fenceAgents[strings.TrimPrefix(resource.Id, fenceAgentPrefix)] = placeHolder
		}
	}
	if len(fenceAgents) == 0 {
		r.Log.Info("fencing not found among resources", "resources", resources)
	}
	return fenceAgents, nil

}

// createDeployments adds an already existing deployments (on the cluster) to the HA Layer pod
func (r *HALayerSetReconciler) createDeployments(deploymentsToCreate []string, namespace string) error {
	//No deployments to create
	if len(deploymentsToCreate) == 0 {
		r.Log.Info("no deployments to create")
		return nil
	}

	for _, dep := range deploymentsToCreate {

		if err := r.verifyDeployment(dep, namespace); err != nil {
			return err
		}

		resourceName := fmt.Sprintf("%s%s", deploymentResourcePrefix, dep)
		pod, err := r.getHAPod(namespace)
		if err != nil {
			return err
		}

		_, _, err = r.execCmdOnPacemaker([]string{"pcs", "resource", "create", resourceName, "k8sDeployment", fmt.Sprintf("deployment=%s", dep), fmt.Sprintf("namespace=%s", namespace), "--force"}, pod)
		if err != nil {
			r.Log.Error(err, "Can't create resource on HA Layer pod in order to manage the deployment", "HA Layer pod template name", haLayerPodTemplateName, "resource name", resourceName, "deployment name", dep)
			return err
		}
		r.Log.Info("deployment created", "deployment name", resourceName)

	}

	return nil
}

func (r *HALayerSetReconciler) deleteDeployments(deploymentsToDelete []string, namespace string) error {

	//No deployments to delete
	if len(deploymentsToDelete) == 0 {
		r.Log.Info("no deployments to delete")
		return nil
	}

	for _, dep := range deploymentsToDelete {

		if err := r.verifyDeployment(dep, namespace); err != nil {
			return err
		}

		resourceName := fmt.Sprintf("%s%s", deploymentResourcePrefix, dep)
		if err := r.removeResource(resourceName, namespace); err != nil {
			return err
		}
		r.Log.Info("deployment deleted", "deployment name", resourceName)
	}

	return nil
}

func (r *HALayerSetReconciler) removeResource(resourceName string, namespace string) error {

	pod, err := r.getHAPod(namespace)
	if err != nil {
		return err
	}

	_, _, err = r.execCmdOnPacemaker([]string{"pcs", "resource", "remove", resourceName, "--force"}, pod)
	if err != nil {
		r.Log.Info("Can't remove resource, trying cleanup", "resource name", resourceName)
		_, _, err = r.execCmdOnPacemaker([]string{"pcs", "resource", "cleanup", resourceName}, pod)
		if err != nil {
			r.Log.Error(err, "Can't delete resource on HA Layer pod ", "HA Layer pod template name", haLayerPodTemplateName, "resource name", resourceName)
			return err
		}
	}
	r.Log.Info("resource deleted", "resource name", resourceName)
	return nil
}

func (r *HALayerSetReconciler) createFenceAgents(hals *appv1alpha1.HALayerSet) error {
	for _, fenceAgent := range hals.Spec.FenceAgentsSpec {
		if err := r.createFenceAgent(fenceAgent, hals.Namespace); err != nil {
			return err
		}
	}
	return nil
}
