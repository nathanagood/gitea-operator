package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

var _ = Describe("Repo validation", func() {
	ctx := context.Background()

	create := func(name string, spec hyperv1.RepoSpec) *hyperv1.Repo {
		repo := &hyperv1.Repo{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: DEFAULT},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, repo)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, repo)).To(Succeed()) })
		return repo
	}

	It("accepts a Gitea name that is not a valid Kubernetes name", func() {
		create("itz-auto-capacity-server", hyperv1.RepoSpec{
			Name: "auto-capacity_server", Org: &hyperv1.OrgRef{Name: "itz"},
		})
	})

	It("rejects characters Gitea does not allow in spec.name", func() {
		repo := &hyperv1.Repo{
			ObjectMeta: metav1.ObjectMeta{Name: "itz-bad", Namespace: DEFAULT},
			Spec:       hyperv1.RepoSpec{Name: "bad name", Org: &hyperv1.OrgRef{Name: "itz"}},
		}
		Expect(k8sClient.Create(ctx, repo)).NotTo(Succeed())
	})

	It("rejects changing spec.name", func() {
		repo := create("itz-tools", hyperv1.RepoSpec{Name: "tools", Org: &hyperv1.OrgRef{Name: "itz"}})
		repo.Spec.Name = "tools2"
		Expect(k8sClient.Update(ctx, repo)).To(MatchError(ContainSubstring("cannot be changed")))
	})

	It("allows setting spec.name to the name already in use", func() {
		repo := create("tools", hyperv1.RepoSpec{Org: &hyperv1.OrgRef{Name: "itz"}})
		repo.Spec.Name = "tools"
		Expect(k8sClient.Update(ctx, repo)).To(Succeed())
	})

	It("rejects moving a repo to another org", func() {
		repo := create("itz-docs", hyperv1.RepoSpec{Name: "docs", Org: &hyperv1.OrgRef{Name: "itz"}})
		repo.Spec.Org.Name = "itz-content"
		Expect(k8sClient.Update(ctx, repo)).To(MatchError(ContainSubstring("spec.org.name cannot be changed")))
	})

	It("allows archiving", func() {
		repo := create("itz-archive-me", hyperv1.RepoSpec{Name: "archive-me", Org: &hyperv1.OrgRef{Name: "itz"}})
		repo.Spec.Archived = true
		Expect(k8sClient.Update(ctx, repo)).To(Succeed())
	})
})
