/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	g "code.gitea.io/sdk/gitea"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
	hclient "hyperspike.io/gitea-operator/internal/client"
)

const repoFinalizer = "repo.hyperspike.io/finalizer"

const (
	// repoNotReadyRequeue is how soon to retry while Gitea is not ready.
	repoNotReadyRequeue = 5 * time.Second
	// repoResync is how often a synced repository is checked for drift, such
	// as settings changed in the web UI.
	repoResync = 10 * time.Minute
)

// RepoReconciler reconciles a Repo object
type RepoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// repoAPI is the part of the Gitea API the Repo controller uses.
type repoAPI interface {
	GetRepo(owner, name string) (*g.Repository, *g.Response, error)
	CreateOrgRepo(org string, opt g.CreateRepoOption) (*g.Repository, *g.Response, error)
	EditRepo(owner, name string, opt g.EditRepoOption) (*g.Repository, *g.Response, error)
	DeleteRepo(owner, name string) (*g.Response, error)
	PushMirrors(owner, name string, opt g.CreatePushMirrorOption) (*g.PushMirrorResponse, *g.Response, error)
}

// +kubebuilder:rbac:groups=hyperspike.io,resources=repoes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hyperspike.io,resources=repoes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hyperspike.io,resources=repoes/finalizers,verbs=update

// Reconcile creates the org repository if it is missing and keeps its
// description, visibility and archived state in line with the spec. Deleting
// the Repo deletes the repository in Gitea.
func (r *RepoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	repo := &hyperv1.Repo{}
	if err := r.Get(ctx, req.NamespacedName, repo); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if repo.Spec.Org == nil || repo.Spec.Org.Name == "" {
		if repo.GetDeletionTimestamp() != nil {
			return ctrl.Result{}, r.removeFinalizer(ctx, repo)
		}
		return ctrl.Result{}, errors.New("spec.org.name is required; user-owned repositories are not supported")
	}

	h, _, err := hclient.BuildFromOrg(ctx, r.Client, repo.Spec.Org, repo.Namespace)
	if repo.GetDeletionTimestamp() != nil {
		return r.finalize(ctx, repo, h, err)
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if h == nil {
		return ctrl.Result{RequeueAfter: repoNotReadyRequeue}, nil
	}

	// Add the finalizer before creating anything, so a repository is never
	// created without one.
	if !controllerutil.ContainsFinalizer(repo, repoFinalizer) {
		controllerutil.AddFinalizer(repo, repoFinalizer)
		if err := r.Update(ctx, repo); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := syncRepo(ctx, h, repo); err != nil {
		return ctrl.Result{}, err
	}
	if !repo.Status.Provisioned {
		repo.Status.Provisioned = true
		if err := r.Status().Update(ctx, repo); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: repoResync}, nil
}

// finalize deletes the repository in Gitea and releases the Repo. buildErr is
// the error from building the Gitea client: when the Org or Gitea resource is
// already gone there is nothing left to delete from, so the Repo is released.
func (r *RepoReconciler) finalize(ctx context.Context, repo *hyperv1.Repo, h *hclient.Client, buildErr error) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(repo, repoFinalizer) {
		return ctrl.Result{}, nil
	}
	switch {
	case apierrors.IsNotFound(buildErr):
		log.FromContext(ctx).Info("org or Gitea instance is gone; releasing repo without deleting it in Gitea",
			"org", repo.Spec.Org.Name, "repo", repo.GiteaName())
	case buildErr != nil:
		return ctrl.Result{}, buildErr
	case h == nil:
		return ctrl.Result{RequeueAfter: repoNotReadyRequeue}, nil
	default:
		if err := deleteGiteaRepo(h, repo.Spec.Org.Name, repo.GiteaName()); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, r.removeFinalizer(ctx, repo)
}

func (r *RepoReconciler) removeFinalizer(ctx context.Context, repo *hyperv1.Repo) error {
	if !controllerutil.RemoveFinalizer(repo, repoFinalizer) {
		return nil
	}
	return r.Update(ctx, repo)
}

// syncRepo creates the org repository if it does not exist, then applies any
// differences in description, visibility and archived state.
func syncRepo(ctx context.Context, api repoAPI, repo *hyperv1.Repo) error {
	owner, name := repo.Spec.Org.Name, repo.GiteaName()
	current, resp, err := api.GetRepo(owner, name)
	switch {
	case err == nil:
	case isNotFound(resp):
		if current, err = createGiteaRepo(ctx, api, owner, name, &repo.Spec); err != nil {
			return err
		}
	default:
		return fmt.Errorf("getting repository %s/%s: %w", owner, name, err)
	}

	edit, changed := repoEdits(current, &repo.Spec)
	if !changed {
		return nil
	}
	log.FromContext(ctx).Info("updating repository", "org", owner, "repo", name)
	if _, _, err := api.EditRepo(owner, name, edit); err != nil {
		return fmt.Errorf("updating repository %s/%s: %w", owner, name, err)
	}
	return nil
}

// createGiteaRepo creates the repository and, if the spec asks for one, its
// push mirror. The creation-only settings (readme, license, default branch,
// and so on) are applied here and never again.
func createGiteaRepo(ctx context.Context, api repoAPI, owner, name string, spec *hyperv1.RepoSpec) (*g.Repository, error) {
	logger := log.FromContext(ctx)
	logger.Info("creating repository", "org", owner, "repo", name)
	created, _, err := api.CreateOrgRepo(owner, g.CreateRepoOption{
		Name:          name,
		Description:   spec.Description,
		Private:       spec.Private,
		Readme:        spec.Readme,
		License:       spec.License,
		DefaultBranch: spec.DefaultBranch,
		Gitignores:    spec.Gitignores,
		Template:      spec.Template,
		AutoInit:      spec.AutoInit,
		IssueLabels:   spec.IssueLabels,
	})
	if err != nil {
		return nil, fmt.Errorf("creating repository %s/%s: %w", owner, name, err)
	}
	if spec.Mirror != nil && spec.Mirror.RemoteURL != "" {
		if _, _, err := api.PushMirrors(owner, name, g.CreatePushMirrorOption{
			Interval:       spec.Mirror.Interval,
			RemoteAddress:  spec.Mirror.RemoteURL,
			SyncONCommit:   spec.Mirror.SyncOnCommit,
			RemoteUsername: spec.Mirror.UserName,
			RemotePassword: spec.Mirror.Password,
		}); err != nil {
			return nil, fmt.Errorf("creating push mirror for %s/%s: %w", owner, name, err)
		}
	}
	return created, nil
}

// repoEdits returns the edit that brings current in line with spec, and
// whether there is anything to change.
func repoEdits(current *g.Repository, spec *hyperv1.RepoSpec) (g.EditRepoOption, bool) {
	var edit g.EditRepoOption
	changed := false
	if current.Description != spec.Description {
		edit.Description = &spec.Description
		changed = true
	}
	if current.Private != spec.Private {
		edit.Private = &spec.Private
		changed = true
	}
	if current.Archived != spec.Archived {
		edit.Archived = &spec.Archived
		changed = true
	}
	return edit, changed
}

// deleteGiteaRepo deletes the repository; one that is already gone is not an
// error.
func deleteGiteaRepo(api repoAPI, owner, name string) error {
	resp, err := api.DeleteRepo(owner, name)
	if err != nil && !isNotFound(resp) {
		return fmt.Errorf("deleting repository %s/%s: %w", owner, name, err)
	}
	return nil
}

func isNotFound(resp *g.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusNotFound
}

// SetupWithManager sets up the controller with the Manager.
func (r *RepoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hyperv1.Repo{}).
		Complete(r)
}
