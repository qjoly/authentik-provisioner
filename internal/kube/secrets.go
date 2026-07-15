// Package kube writes Kubernetes secrets from within (or outside) a cluster.
// It is only used when a provider declares a secretName; otherwise the
// provisioner never touches the cluster and needs no Kubernetes access.
package kube

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// SecretWriter upserts opaque secrets in a fixed namespace.
type SecretWriter struct {
	client    kubernetes.Interface
	namespace string
}

// NewSecretWriter builds a writer using the in-cluster config when available,
// falling back to KUBECONFIG / ~/.kube/config for local runs. The namespace is
// taken from POD_NAMESPACE, then the service-account namespace file, then
// "default".
func NewSecretWriter() (*SecretWriter, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		cfg, err = kubeconfigFallback()
		if err != nil {
			return nil, fmt.Errorf("no kubernetes config (in-cluster and kubeconfig both failed): %w", err)
		}
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &SecretWriter{client: client, namespace: detectNamespace()}, nil
}

func kubeconfigFallback() (*rest.Config, error) {
	path := os.Getenv("KUBECONFIG")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".kube", "config")
	}
	return clientcmd.BuildConfigFromFlags("", path)
}

func detectNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	const saPath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	if data, err := os.ReadFile(saPath); err == nil && len(data) > 0 {
		return string(data)
	}
	return "default"
}

// Namespace returns the namespace secrets are written to.
func (w *SecretWriter) Namespace() string { return w.namespace }

// Upsert creates or updates an opaque secret with the given string data.
func (w *SecretWriter) Upsert(ctx context.Context, name string, data map[string]string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: w.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "authentik-provisioner",
			},
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: data,
	}

	secrets := w.client.CoreV1().Secrets(w.namespace)
	_, err := secrets.Create(ctx, secret, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	// Already exists: update in place.
	existing, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	existing.StringData = data
	existing.Type = corev1.SecretTypeOpaque
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels["app.kubernetes.io/managed-by"] = "authentik-provisioner"
	_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}
