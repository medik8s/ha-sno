package controllers

import (
	"context"
	"fmt"
	"github.com/mshitrit/hasno-setup-operator/api/v1alpha1"
	appv1alpha1 "github.com/mshitrit/hasno-setup-operator/api/v1alpha1"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var execCmdCommands = make(chan execCmdCommand, 1000)

type SpyHALayerSetReconciler struct {
	*HALayerSetReconciler
}

func (r *SpyHALayerSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.HALayerSetReconciler.Reconcile(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SpyHALayerSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.HALayerSetReconciler.pacemakerCommandHandler = &mockPacemakerCommandHandler{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1alpha1.HALayerSet{}).
		Complete(r)
}

type mockPacemakerCommandHandler struct{}

//Don't remove podPhase, it is necessary in order to implement pacemakerCommandHandler
func (r *mockPacemakerCommandHandler) isPodRunning(podPhase corev1.PodPhase) bool {
	return true
}

func (r *mockPacemakerCommandHandler) execCmdOnPacemaker(command []string, pod *corev1.Pod) (stdout, stderr string, err error) {
	execCmdCommands <- execCmdCommand{command, pod}
	return "", "", nil
}

//Don't remove result, it is necessary in order to implement pacemakerCommandHandler
func (r *mockPacemakerCommandHandler) extractHAStatus(result string) (*haStatus, error) {
	h := new(haStatus)
	h.Resources = resources{
		Resources: []resource{
			{Id: originalFenceAgentName, ResourceAgent: fmt.Sprintf("%s:mock-fence-agent", pcsFenceIdentifier)},
		}}
	return h, nil
}

func createHALayerPod() *corev1.Pod {
	hals := &v1alpha1.HALayerSet{ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace}, Spec: v1alpha1.HALayerSetSpec{NodesSpec: v1alpha1.NodesSpec{FirstNodeName: node1Name, FirstNodeIP: "192.168.126.10", SecondNodeName: node2Name, SecondNodeIP: "192.168.126.11"}}}
	pod := spyReconciler.buildHALayerPod(hals, node1Name)
	pod.Name = createPodTemplateNameFromCRName(haLayerCRName)
	return pod
}

func (r *mockPacemakerCommandHandler) postDeploymentCreateHook() {
	Expect(k8sClient.Create(context.Background(), createHALayerPod())).ToNot(HaveOccurred())
}

func (r *mockPacemakerCommandHandler) postDeploymentDeleteHook() {
	if pod, err := getHALayerPod(); err == nil {
		Expect(k8sClient.Delete(context.Background(), pod)).ToNot(HaveOccurred())
	} else {
		Expect(errors.IsNotFound(err)).To(BeTrue())
	}
}

type execCmdCommand struct {
	command []string
	pod     *corev1.Pod
}
