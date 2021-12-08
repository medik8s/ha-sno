package controllers

import (
	"bytes"
	"encoding/xml"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// pacemakerCommandHandler executes commands on the pacemaker container within the pod
type pacemakerCommandHandler interface {
	isPodRunning(podPhase corev1.PodPhase) bool
	execCmdOnPacemaker(command []string, pod *corev1.Pod) (stdout, stderr string, err error)
	extractHAStatus(result string) (*haStatus, error)
	postDeploymentCreateHook()
	postDeploymentDeleteHook()
}

type basePacemakerCommandHandler struct {
	Log logr.Logger
}

func (r *basePacemakerCommandHandler) isPodRunning(podPhase corev1.PodPhase) bool {
	return podPhase == corev1.PodRunning
}

// execCmdOnPacemaker exec command on specific pod and wait the command's output.
func (r *basePacemakerCommandHandler) execCmdOnPacemaker(command []string, pod *corev1.Pod) (stdout, stderr string, err error) {
	if err := r.buildK8sClient(); err != nil {
		return "", "", err
	}

	var (
		stdoutBuf bytes.Buffer
		stderrBuf bytes.Buffer
	)

	req := kClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		Param("container", containerNamePacemaker)

	req.VersionedParams(&corev1.PodExecOptions{
		Container: containerNamePacemaker,
		Command:   command,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)
	execSPDY, err := remotecommand.NewSPDYExecutor(k8sClientConfig, "POST", req.URL())
	if err != nil {
		r.Log.Error(err, "failed building SPDY (shell) executor")
		return "", "", err
	}
	err = execSPDY.Stream(remotecommand.StreamOptions{
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
		Tty:    false,
	})
	if err != nil {
		r.Log.Error(err, "Failed to run exec command", "stdout", stdoutBuf.String(), "stderr", stderrBuf.String())
	}
	return stdoutBuf.String(), stderrBuf.String(), err
}

func (r *basePacemakerCommandHandler) buildK8sClient() error {
	//client was already built stop here
	if kClient != nil {
		return nil
	}

	if config, err := restclient.InClusterConfig(); err != nil {
		r.Log.Error(err, "failed getting cluster config")
		return err
	} else {
		k8sClientConfig = config

		if clientSet, err := kubernetes.NewForConfig(k8sClientConfig); err != nil {
			r.Log.Error(err, "failed building k8s client")
			return err
		} else {
			kClient = clientSet
		}
	}
	return nil
}

func (r *basePacemakerCommandHandler) extractHAStatus(result string) (*haStatus, error) {
	var status haStatus
	if err := xml.Unmarshal([]byte(result), &status); err != nil {
		r.Log.Error(err, "failed to unmarshal the status of the HA layer")
		return nil, err
	} else {
		return &status, nil
	}
}

func (r *basePacemakerCommandHandler) postDeploymentCreateHook() {
	//Nothing to do here, at the moment this is only used as a hook to modify UT behaviour
}

func (r *basePacemakerCommandHandler) postDeploymentDeleteHook() {
	//Nothing to do here, at the moment this is only used as a hook to modify UT behaviour
}
