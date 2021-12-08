package controllers

import (
	"context"
	"fmt"
	appv1alpha1 "github.com/mshitrit/hasno-setup-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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

func (r *mockPacemakerCommandHandler) isPodRunning(podPhase corev1.PodPhase) bool {
	return true
}

func (r *mockPacemakerCommandHandler) execCmdOnPacemaker(command []string, pod *corev1.Pod) (stdout, stderr string, err error) {
	execCmdCommands <- execCmdCommand{command, pod}
	return "", "", nil
}
func (r *mockPacemakerCommandHandler) extractHAStatus(result string) (*haStatus, error) {
	h := new(haStatus)
	h.Resources = resources{
		Resources: []resource{
			{Id: originalFenceAgentName, ResourceAgent: fmt.Sprintf("%s:mock-fence-agent", pcsFenceIdentifier)},
		}}
	return h, nil
}

type execCmdCommand struct {
	command []string
	pod     *corev1.Pod
}
