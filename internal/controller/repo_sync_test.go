package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	g "code.gitea.io/sdk/gitea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

// fakeRepoAPI is an in-memory Gitea holding repositories keyed by "owner/name".
type fakeRepoAPI struct {
	repos   map[string]*g.Repository
	creates []g.CreateRepoOption
	edits   []g.EditRepoOption
	mirrors []g.CreatePushMirrorOption
	deletes []string
	getErr  error
}

func newFakeRepoAPI(existing ...*g.Repository) *fakeRepoAPI {
	f := &fakeRepoAPI{repos: map[string]*g.Repository{}}
	for _, r := range existing {
		f.repos[r.FullName] = r
	}
	return f
}

func response(status int) *g.Response {
	return &g.Response{Response: &http.Response{StatusCode: status}}
}

func (f *fakeRepoAPI) GetRepo(owner, name string) (*g.Repository, *g.Response, error) {
	if f.getErr != nil {
		return nil, response(http.StatusInternalServerError), f.getErr
	}
	if r, ok := f.repos[owner+"/"+name]; ok {
		copied := *r
		return &copied, response(http.StatusOK), nil
	}
	return nil, response(http.StatusNotFound), errors.New("404 Not Found")
}

func (f *fakeRepoAPI) CreateOrgRepo(org string, opt g.CreateRepoOption) (*g.Repository, *g.Response, error) {
	f.creates = append(f.creates, opt)
	r := &g.Repository{FullName: org + "/" + opt.Name, Name: opt.Name, Description: opt.Description, Private: opt.Private}
	f.repos[r.FullName] = r
	copied := *r
	return &copied, response(http.StatusCreated), nil
}

func (f *fakeRepoAPI) EditRepo(owner, name string, opt g.EditRepoOption) (*g.Repository, *g.Response, error) {
	f.edits = append(f.edits, opt)
	r := f.repos[owner+"/"+name]
	if opt.Description != nil {
		r.Description = *opt.Description
	}
	if opt.Private != nil {
		r.Private = *opt.Private
	}
	if opt.Archived != nil {
		r.Archived = *opt.Archived
	}
	return r, response(http.StatusOK), nil
}

func (f *fakeRepoAPI) DeleteRepo(owner, name string) (*g.Response, error) {
	key := owner + "/" + name
	if _, ok := f.repos[key]; !ok {
		return response(http.StatusNotFound), errors.New("404 Not Found")
	}
	f.deletes = append(f.deletes, key)
	delete(f.repos, key)
	return response(http.StatusNoContent), nil
}

func (f *fakeRepoAPI) PushMirrors(_, _ string, opt g.CreatePushMirrorOption) (*g.PushMirrorResponse, *g.Response, error) {
	f.mirrors = append(f.mirrors, opt)
	return &g.PushMirrorResponse{}, response(http.StatusOK), nil
}

func newRepo(metaName string, spec hyperv1.RepoSpec) *hyperv1.Repo {
	if spec.Org == nil {
		spec.Org = &hyperv1.OrgRef{Name: "itz"}
	}
	return &hyperv1.Repo{ObjectMeta: metav1.ObjectMeta{Name: metaName}, Spec: spec}
}

func TestGiteaNameDefaultsToMetadataName(t *testing.T) {
	if got := newRepo("tools", hyperv1.RepoSpec{}).GiteaName(); got != "tools" {
		t.Fatalf("GiteaName() = %q, want %q", got, "tools")
	}
	if got := newRepo("itz-auto-capacity-server", hyperv1.RepoSpec{Name: "auto-capacity_server"}).GiteaName(); got != "auto-capacity_server" {
		t.Fatalf("GiteaName() = %q, want %q", got, "auto-capacity_server")
	}
}

func TestSyncRepoCreatesMissingRepoUnderSpecName(t *testing.T) {
	api := newFakeRepoAPI()
	repo := newRepo("itz-auto-ocpv", hyperv1.RepoSpec{
		Name: "auto-ocpv", Description: "d", Private: true, DefaultBranch: "main",
		Mirror: &hyperv1.MirrorSpec{RemoteURL: "https://example.com/m.git"},
	})

	if err := syncRepo(context.Background(), api, "itz", repo); err != nil {
		t.Fatal(err)
	}
	if len(api.creates) != 1 || api.creates[0].Name != "auto-ocpv" || api.creates[0].DefaultBranch != "main" {
		t.Fatalf("creates = %+v, want one create of auto-ocpv with default branch main", api.creates)
	}
	if _, ok := api.repos["itz/auto-ocpv"]; !ok {
		t.Fatalf("repo not created under the org with spec.name; have %v", api.repos)
	}
	if len(api.mirrors) != 1 {
		t.Fatalf("mirrors = %d, want 1", len(api.mirrors))
	}
	if len(api.edits) != 0 {
		t.Fatalf("unexpected edits after create: %+v", api.edits)
	}
}

func TestSyncRepoLeavesMatchingRepoAlone(t *testing.T) {
	api := newFakeRepoAPI(&g.Repository{FullName: "itz/tools", Description: "d", Private: true})
	repo := newRepo("itz-tools", hyperv1.RepoSpec{Name: "tools", Description: "d", Private: true})

	if err := syncRepo(context.Background(), api, "itz", repo); err != nil {
		t.Fatal(err)
	}
	if len(api.creates)+len(api.edits)+len(api.mirrors) != 0 {
		t.Fatalf("expected no calls, got creates=%d edits=%d mirrors=%d", len(api.creates), len(api.edits), len(api.mirrors))
	}
}

func TestSyncRepoAppliesOnlyChangedFields(t *testing.T) {
	api := newFakeRepoAPI(&g.Repository{FullName: "itz/tools", Description: "old", Private: true})
	repo := newRepo("itz-tools", hyperv1.RepoSpec{Name: "tools", Description: "new", Private: true})

	if err := syncRepo(context.Background(), api, "itz", repo); err != nil {
		t.Fatal(err)
	}
	if len(api.edits) != 1 {
		t.Fatalf("edits = %d, want 1", len(api.edits))
	}
	e := api.edits[0]
	if e.Description == nil || *e.Description != "new" || e.Private != nil || e.Archived != nil {
		t.Fatalf("edit = %+v, want only description=new", e)
	}
}

func TestSyncRepoArchivesAndUnarchives(t *testing.T) {
	api := newFakeRepoAPI(&g.Repository{FullName: "itz/tools"})
	repo := newRepo("itz-tools", hyperv1.RepoSpec{Name: "tools", Archived: true})

	if err := syncRepo(context.Background(), api, "itz", repo); err != nil {
		t.Fatal(err)
	}
	if !api.repos["itz/tools"].Archived {
		t.Fatal("repo not archived")
	}

	repo.Spec.Archived = false
	if err := syncRepo(context.Background(), api, "itz", repo); err != nil {
		t.Fatal(err)
	}
	if api.repos["itz/tools"].Archived {
		t.Fatal("repo not unarchived")
	}
}

func TestSyncRepoCreatesThenArchives(t *testing.T) {
	api := newFakeRepoAPI()
	repo := newRepo("itz-old", hyperv1.RepoSpec{Name: "old", Archived: true})

	if err := syncRepo(context.Background(), api, "itz", repo); err != nil {
		t.Fatal(err)
	}
	if len(api.creates) != 1 || !api.repos["itz/old"].Archived {
		t.Fatalf("want repo created then archived; creates=%d archived=%v", len(api.creates), api.repos["itz/old"].Archived)
	}
}

func TestSyncRepoReturnsGetErrorsOtherThanNotFound(t *testing.T) {
	api := newFakeRepoAPI()
	api.getErr = errors.New("boom")

	if err := syncRepo(context.Background(), api, "itz", newRepo("itz-tools", hyperv1.RepoSpec{})); err == nil {
		t.Fatal("expected an error")
	}
	if len(api.creates) != 0 {
		t.Fatal("must not create when the lookup failed")
	}
}

func TestDeleteGiteaRepoIgnoresMissingRepo(t *testing.T) {
	api := newFakeRepoAPI(&g.Repository{FullName: "itz/tools"})

	if err := deleteGiteaRepo(api, "itz", "tools"); err != nil {
		t.Fatal(err)
	}
	if err := deleteGiteaRepo(api, "itz", "tools"); err != nil {
		t.Fatalf("second delete of a missing repo: %v", err)
	}
	if len(api.deletes) != 1 {
		t.Fatalf("deletes = %v, want one", api.deletes)
	}
}
