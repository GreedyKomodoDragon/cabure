package controller

import (
	"context"
	"testing"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestLoadCredentialsReadsSecretFromApplicationNamespace(t *testing.T) {
	t.Parallel()

	app := &v1alpha1.GitApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "app-ns",
		},
		Spec: v1alpha1.GitApplicationSpec{
			Source: v1alpha1.GitSourceSpec{
				SecretRef: &corev1.LocalObjectReference{Name: "repo-creds"},
			},
		},
	}

	reconciler := &GitApplicationReconciler{
		Kube: kubefake.NewSimpleClientset(
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "repo-creds",
					Namespace: "app-ns",
				},
				Type: corev1.SecretTypeSSHAuth,
				Data: map[string][]byte{
					"ssh-privatekey": []byte("app-key"),
					"known_hosts":    []byte("app-hosts"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "repo-creds",
					Namespace: "other-ns",
				},
				Type: corev1.SecretTypeSSHAuth,
				Data: map[string][]byte{
					"ssh-privatekey": []byte("other-key"),
					"known_hosts":    []byte("other-hosts"),
				},
			},
		),
	}

	creds, err := reconciler.loadCredentials(context.Background(), app)
	if err != nil {
		t.Fatalf("loadCredentials returned error: %v", err)
	}
	if got, want := string(creds.SSHPrivateKey), "app-key"; got != want {
		t.Fatalf("SSHPrivateKey = %q, want %q", got, want)
	}
	if got, want := string(creds.KnownHosts), "app-hosts"; got != want {
		t.Fatalf("KnownHosts = %q, want %q", got, want)
	}
}
