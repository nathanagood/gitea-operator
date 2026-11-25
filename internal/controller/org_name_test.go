package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

var _ = Describe("Org Gitea name", func() {
	ctx := context.Background()

	newOrg := func(name, giteaName string) *hyperv1.Org {
		org := &hyperv1.Org{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: DEFAULT},
			Spec:       hyperv1.OrgSpec{Name: giteaName, Instance: hyperv1.InstanceType{Name: "git"}},
		}
		Expect(k8sClient.Create(ctx, org)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, org)).To(Succeed()) })
		return org
	}

	It("accepts a Gitea name with capital letters and uses it", func() {
		org := newOrg("itz-upper", "ITZ")
		Expect(org.GiteaName()).To(Equal("ITZ"))
	})

	It("defaults to metadata.name", func() {
		Expect(newOrg("itz-plain", "").GiteaName()).To(Equal("itz-plain"))
	})

	It("rejects changing spec.name", func() {
		org := newOrg("itz-fixed", "ITZ")
		org.Spec.Name = "ITZ2"
		Expect(k8sClient.Update(ctx, org)).To(MatchError(ContainSubstring("cannot be changed")))
	})

	It("allows fixing the letter case of an existing name", func() {
		org := newOrg("itz", "")
		org.Spec.Name = "ITZ"
		Expect(k8sClient.Update(ctx, org)).To(Succeed())
	})

	It("rejects characters Gitea does not allow", func() {
		org := &hyperv1.Org{
			ObjectMeta: metav1.ObjectMeta{Name: "itz-bad", Namespace: DEFAULT},
			Spec:       hyperv1.OrgSpec{Name: "I T Z", Instance: hyperv1.InstanceType{Name: "git"}},
		}
		Expect(k8sClient.Create(ctx, org)).NotTo(Succeed())
	})

	It("gives a Repo its Org's Gitea name as the owner", func() {
		newOrg("itz-owner", "ITZ")
		repo := &hyperv1.Repo{
			ObjectMeta: metav1.ObjectMeta{Name: "itz-owner-tools", Namespace: DEFAULT},
			Spec:       hyperv1.RepoSpec{Name: "tools", Org: &hyperv1.OrgRef{Name: "itz-owner"}},
		}
		r := &RepoReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.giteaOwner(ctx, repo)).To(Equal("ITZ"))
	})
})
