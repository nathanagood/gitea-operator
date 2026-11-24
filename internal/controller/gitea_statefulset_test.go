package controller

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

func testGitea(image string) *hyperv1.Gitea {
	return &hyperv1.Gitea{
		ObjectMeta: metav1.ObjectMeta{Name: "git", Namespace: DEFAULT},
		Spec: hyperv1.GiteaSpec{
			Image:    image,
			TLS:      true,
			Postgres: hyperv1.PostgresSpec{Provider: Cnpg},
		},
	}
}

func TestGiteaStatefulSetHasOnlyTheGiteaContainer(t *testing.T) {
	sts := giteaStatefulSet(testGitea("gitea/gitea:1.27.3"))

	containers := sts.Spec.Template.Spec.Containers
	if len(containers) != 1 || containers[0].Name != Gitea {
		names := make([]string, 0, len(containers))
		for _, c := range containers {
			names = append(names, c.Name)
		}
		t.Fatalf("containers = %v, want only %q", names, Gitea)
	}
	if containers[0].Image != "gitea/gitea:1.27.3" {
		t.Fatalf("image = %q", containers[0].Image)
	}
}

func TestPodTemplateHashIsStable(t *testing.T) {
	first, err := podTemplateHash(&giteaStatefulSet(testGitea("gitea/gitea:1.27.3")).Spec.Template)
	if err != nil {
		t.Fatal(err)
	}
	// Map iteration order is random; build it many times to catch any
	// ordering that leaks into the template.
	for range 50 {
		got, err := podTemplateHash(&giteaStatefulSet(testGitea("gitea/gitea:1.27.3")).Spec.Template)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Fatalf("hash changed between builds: %s != %s", got, first)
		}
	}
	other, _ := podTemplateHash(&giteaStatefulSet(testGitea("gitea/gitea:1.27.4")).Spec.Template)
	if other == first {
		t.Fatal("hash did not change with the image")
	}
}

func TestApplyPodTemplateKeepsForeignAnnotations(t *testing.T) {
	current := giteaStatefulSet(testGitea("gitea/gitea:1.27.3"))
	current.Spec.Template.Annotations = map[string]string{
		"auth.hyperspike.io/default.verify":       "abc",
		"kubectl.kubernetes.io/restartedAt":       "2026-11-24T00:00:00Z",
		"unrelated.example.com/keep-this-as-well": "yes",
	}
	desired := giteaStatefulSet(testGitea("gitea/gitea:1.27.4"))

	applyPodTemplate(current, &desired.Spec.Template, "h1")

	if got := current.Spec.Template.Spec.Containers[0].Image; got != "gitea/gitea:1.27.4" {
		t.Fatalf("image = %q, want the desired image", got)
	}
	for k, v := range map[string]string{
		"auth.hyperspike.io/default.verify":       "abc",
		"kubectl.kubernetes.io/restartedAt":       "2026-11-24T00:00:00Z",
		"unrelated.example.com/keep-this-as-well": "yes",
	} {
		if current.Spec.Template.Annotations[k] != v {
			t.Fatalf("annotation %s = %q, want %q", k, current.Spec.Template.Annotations[k], v)
		}
	}
	if current.Annotations[appliedHashAnnotation] != "h1" {
		t.Fatalf("hash annotation = %q", current.Annotations[appliedHashAnnotation])
	}
}

var _ = Describe("Gitea StatefulSet", func() {
	ctx := context.Background()

	It("creates, updates in place, and leaves an unchanged StatefulSet alone", func() {
		gitea := testGitea("gitea/gitea:1.27.3")
		Expect(k8sClient.Create(ctx, gitea)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, gitea)).To(Succeed()) })

		r := &GiteaReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: events.NewFakeRecorder(10)}
		key := types.NamespacedName{Name: gitea.Name, Namespace: gitea.Namespace}

		By("creating it")
		Expect(r.upsertGiteaSts(ctx, gitea)).To(Succeed())
		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, sts)).To(Succeed()) })
		Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))

		By("not writing when nothing changed")
		version := sts.ResourceVersion
		Expect(r.upsertGiteaSts(ctx, gitea)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		Expect(sts.ResourceVersion).To(Equal(version))

		By("keeping the Auth restart annotation across an image change")
		sts.Spec.Template.Annotations = map[string]string{"auth.hyperspike.io/default.verify": "abc"}
		Expect(k8sClient.Update(ctx, sts)).To(Succeed())
		gitea.Spec.Image = "gitea/gitea:1.27.4"
		Expect(r.upsertGiteaSts(ctx, gitea)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[0].Image).To(Equal("gitea/gitea:1.27.4"))
		Expect(sts.Spec.Template.Annotations).To(HaveKeyWithValue("auth.hyperspike.io/default.verify", "abc"))
	})
})

func TestGiteaIngressTLSByClass(t *testing.T) {
	gitea := testGitea("gitea/gitea:1.27.3")
	gitea.Spec.TLS = false

	if got := giteaIngress(gitea).Spec.TLS[0].SecretName; got != "git-ingress-tls" {
		t.Fatalf("default class: TLS secret = %q, want git-ingress-tls", got)
	}

	gitea.Spec.Ingress.ClassName = openShiftIngressClass
	ing := giteaIngress(gitea)
	if len(ing.Spec.TLS) != 0 {
		t.Fatalf("openshift-default: TLS = %+v, want none", ing.Spec.TLS)
	}
	if got := ing.Annotations[routeTerminationAnnotation]; got != "edge" {
		t.Fatalf("openshift-default: termination = %q, want edge", got)
	}
	if got := *ing.Spec.IngressClassName; got != openShiftIngressClass {
		t.Fatalf("class = %q", got)
	}

	gitea.Spec.Ingress.Annotations = map[string]string{routeTerminationAnnotation: "reencrypt"}
	if got := giteaIngress(gitea).Annotations[routeTerminationAnnotation]; got != "reencrypt" {
		t.Fatalf("user termination overridden: %q", got)
	}

	gitea.Spec.Ingress.Annotations = nil
	gitea.Spec.TLS = true
	if _, set := giteaIngress(gitea).Annotations[routeTerminationAnnotation]; set {
		t.Fatal("termination must be left to the user when Gitea serves HTTPS")
	}
}

func TestGiteaIngressDoesNotModifyTheGiteaSpec(t *testing.T) {
	gitea := testGitea("gitea/gitea:1.27.3")
	gitea.Spec.Ingress.Annotations = map[string]string{"a": "b"}

	giteaIngress(gitea)

	if len(gitea.Spec.Ingress.Annotations) != 1 {
		t.Fatalf("spec annotations modified: %v", gitea.Spec.Ingress.Annotations)
	}
}

func TestRootfulGiteaKeepsExistingSecuritySettings(t *testing.T) {
	sts := giteaStatefulSet(testGitea("gitea/gitea:1.27.3"))
	pod := sts.Spec.Template.Spec

	if pod.SecurityContext != nil {
		t.Fatalf("rootful pod security context = %+v, want none", pod.SecurityContext)
	}
	if caps := pod.Containers[0].SecurityContext.Capabilities.Add; len(caps) != 1 || caps[0] != "SYS_CHROOT" {
		t.Fatalf("rootful gitea capabilities = %v, want [SYS_CHROOT]", caps)
	}
	if !hasEnv(pod.Containers[0].Env, "START_SSH_SERVER", "false") {
		t.Fatal("rootful gitea should use the image's OpenSSH (START_SSH_SERVER=false)")
	}
	if pod.Containers[0].ReadinessProbe == nil {
		t.Fatal("gitea container has no readiness probe")
	}
}

func TestRootlessGiteaRunsUnderRestrictedSettings(t *testing.T) {
	gitea := testGitea("gitea/gitea:1.27.3-rootless")
	gitea.Spec.Rootless = true
	pod := giteaStatefulSet(gitea).Spec.Template.Spec

	if pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Fatalf("pod security context = %+v, want runAsNonRoot", pod.SecurityContext)
	}
	all := append(append([]corev1.Container{}, pod.InitContainers...), pod.Containers...)
	for _, c := range all {
		sc := c.SecurityContext
		if sc == nil || sc.RunAsUser != nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation ||
			sc.Capabilities == nil || len(sc.Capabilities.Add) != 0 || len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
			t.Fatalf("container %s security context = %+v, want no fixed UID, no escalation, drop ALL, add none", c.Name, sc)
		}
	}
	for _, c := range pod.InitContainers {
		if !hasEnv(c.Env, "GITEA_ROOTLESS", "true") {
			t.Fatalf("init container %s lacks GITEA_ROOTLESS=true", c.Name)
		}
	}
	if hasEnv(pod.Containers[0].Env, "START_SSH_SERVER", "false") {
		t.Fatal("rootless gitea must use the built-in SSH server")
	}
}

func hasEnv(env []corev1.EnvVar, name, value string) bool {
	for _, e := range env {
		if e.Name == name && e.Value == value {
			return true
		}
	}
	return false
}
