package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

var _ = Describe("Org status", func() {
	ctx := context.Background()

	It("marks an org that already exists in Gitea as provisioned", func() {
		org := &hyperv1.Org{ObjectMeta: metav1.ObjectMeta{Name: "already-there", Namespace: DEFAULT}}
		Expect(k8sClient.Create(ctx, org)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, org)).To(Succeed()) })
		Expect(org.Status.Provisioned).To(BeFalse())

		r := &OrgReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.markProvisioned(ctx, org)).To(Succeed())

		got := &hyperv1.Org{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: org.Name, Namespace: org.Namespace}, got)).To(Succeed())
		Expect(got.Status.Provisioned).To(BeTrue())

		By("not writing again once provisioned")
		version := got.ResourceVersion
		Expect(r.markProvisioned(ctx, got)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: org.Name, Namespace: org.Namespace}, got)).To(Succeed())
		Expect(got.ResourceVersion).To(Equal(version))
	})
})
