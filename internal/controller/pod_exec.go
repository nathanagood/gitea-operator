package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// PodExecutor runs a command inside a container of a running pod.
type PodExecutor interface {
	Exec(ctx context.Context, namespace, pod, container string, command []string, stdin io.Reader) (stdout string, err error)
}

// SPDYPodExecutor implements PodExecutor through the Kubernetes exec subresource.
type SPDYPodExecutor struct {
	config    *rest.Config
	clientset kubernetes.Interface
}

// NewSPDYPodExecutor builds a PodExecutor from the manager's REST config.
func NewSPDYPodExecutor(config *rest.Config) (*SPDYPodExecutor, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("building clientset for pod exec: %w", err)
	}
	return &SPDYPodExecutor{config: config, clientset: clientset}, nil
}

// Exec runs command in the container and returns its stdout. On failure the
// returned error includes the command's stderr.
func (e *SPDYPodExecutor) Exec(ctx context.Context, namespace, pod, container string, command []string, stdin io.Reader) (string, error) {
	req := e.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(e.config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("creating executor for pod %s/%s: %w", namespace, pod, err)
	}
	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return stdout.String(), fmt.Errorf("exec in pod %s/%s: %w: %s", namespace, pod, err, stderr.String())
	}
	return stdout.String(), nil
}
