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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

const (
	authFinalizer = "auth.hyperspike.io/finalizer"
	// Gitea has no REST API for authentication sources, so they are managed
	// with `gitea admin auth ...` inside the Gitea container. Gitea refuses
	// to run as root, so the rootful image drops to the git user.
	giteaCLIWrapper = `IFS= read -r OAUTH_KEY; IFS= read -r OAUTH_SECRET
if [ "${WITH_CREDENTIALS}" = 1 ]; then set -- "$@" --key "$OAUTH_KEY" --secret "$OAUTH_SECRET"; fi
if [ "$(id -u)" = 0 ]; then exec su-exec git gitea "$@"; fi
exec gitea "$@"`
	authResyncInterval = 10 * time.Minute
	authWaitInterval   = 10 * time.Second
)

var errNoGiteaPod = errors.New("no ready gitea pod found")

// AuthReconciler reconciles an Auth object into a Gitea OAuth2 authentication source.
type AuthReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Executor PodExecutor
}

// giteaAuthSource is one row of `gitea admin auth list`.
type giteaAuthSource struct {
	ID   int64
	Name string
	Type string
}

// +kubebuilder:rbac:groups=hyperspike.io,resources=auths,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hyperspike.io,resources=auths/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hyperspike.io,resources=auths/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile creates or updates the Gitea authentication source described by an Auth.
func (r *AuthReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	auth := &hyperv1.Auth{}
	if err := r.Get(ctx, req.NamespacedName, auth); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	deleting := !auth.DeletionTimestamp.IsZero()
	if deleting && auth.Spec.DeletionPolicy != hyperv1.AuthDeletionDelete {
		return ctrl.Result{}, r.releaseFinalizer(ctx, auth)
	}

	gitea, err := r.giteaInstance(ctx, auth)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !gitea.Status.Ready {
		return ctrl.Result{RequeueAfter: authWaitInterval}, r.setReady(ctx, auth, metav1.ConditionFalse, "InstanceNotReady", "waiting for Gitea instance "+gitea.Name)
	}
	pod, err := r.giteaPod(ctx, gitea)
	if errors.Is(err, errNoGiteaPod) {
		return ctrl.Result{RequeueAfter: authWaitInterval}, r.setReady(ctx, auth, metav1.ConditionFalse, "InstanceNotReady", err.Error())
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if deleting {
		return ctrl.Result{}, r.deleteSource(ctx, gitea, auth, pod)
	}
	if controllerutil.AddFinalizer(auth, authFinalizer) {
		if err := r.Update(ctx, auth); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.applySource(ctx, gitea, auth, pod); err != nil {
		_ = r.setReady(ctx, auth, metav1.ConditionFalse, "ApplyFailed", err.Error())
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: authResyncInterval}, nil
}

// applySource adds or updates the Gitea source when it is missing or its
// configuration (including secret values) changed since the last apply, then
// restarts Gitea so the running server loads it.
func (r *AuthReconciler) applySource(ctx context.Context, gitea *hyperv1.Gitea, auth *hyperv1.Auth, pod *corev1.Pod) error {
	logger := log.FromContext(ctx)

	clientID, err := r.secretValue(ctx, auth.Namespace, auth.Spec.ClientID)
	if err != nil {
		return err
	}
	clientSecret, err := r.secretValue(ctx, auth.Namespace, auth.Spec.ClientSecret)
	if err != nil {
		return err
	}
	flags, err := oauthFlags(auth)
	if err != nil {
		return err
	}
	hash := configHash(flags, clientID, clientSecret)

	existing, err := r.findSource(ctx, pod, sourceName(auth))
	if err != nil {
		return err
	}
	if existing != nil && existing.ID == auth.Status.SourceID && hash == auth.Status.AppliedHash {
		// Also covers an operator upgrade: a source applied before restarts
		// were added gets one restart here, then none.
		if err := r.restartGiteaForAuth(ctx, gitea, auth, hash); err != nil {
			return err
		}
		return r.setReady(ctx, auth, metav1.ConditionTrue, "InSync", "authentication source is up to date")
	}

	var args []string
	if existing == nil {
		logger.Info("adding authentication source", "name", sourceName(auth))
		args = authCommand("add-oauth", flags...)
	} else {
		logger.Info("updating authentication source", "name", sourceName(auth), "id", existing.ID)
		args = authCommand("update-oauth", append([]string{"--id", strconv.FormatInt(existing.ID, 10)}, flags...)...)
	}
	if _, err := r.runGitea(ctx, pod, args, clientID, clientSecret); err != nil {
		return err
	}

	if existing == nil {
		if existing, err = r.findSource(ctx, pod, sourceName(auth)); err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("authentication source %q not found after add-oauth", sourceName(auth))
		}
	}
	if err := r.restartGiteaForAuth(ctx, gitea, auth, hash); err != nil {
		return err
	}
	auth.Status.SourceID = existing.ID
	auth.Status.AppliedHash = hash
	return r.applyStatus(ctx, auth, metav1.ConditionTrue, "Applied", "authentication source applied; restarting Gitea to load it")
}

// deleteSource removes the Gitea source (deletionPolicy: Delete), restarts
// Gitea to unload it, then releases the finalizer.
func (r *AuthReconciler) deleteSource(ctx context.Context, gitea *hyperv1.Gitea, auth *hyperv1.Auth, pod *corev1.Pod) error {
	if !controllerutil.ContainsFinalizer(auth, authFinalizer) {
		return nil
	}
	if auth.Status.SourceID != 0 {
		args := authCommand("delete", "--id", strconv.FormatInt(auth.Status.SourceID, 10))
		if _, err := r.runGitea(ctx, pod, args, "", ""); err != nil {
			return fmt.Errorf("deleting authentication source %d: %w", auth.Status.SourceID, err)
		}
	}
	if err := r.restartGiteaForAuth(ctx, gitea, auth, ""); err != nil {
		return err
	}
	return r.releaseFinalizer(ctx, auth)
}

func (r *AuthReconciler) releaseFinalizer(ctx context.Context, auth *hyperv1.Auth) error {
	if !controllerutil.RemoveFinalizer(auth, authFinalizer) {
		return nil
	}
	return r.Update(ctx, auth)
}

// oauthFlags renders the spec as `gitea admin auth add-oauth` flags. Every
// flag is always passed so update-oauth also clears values removed from the spec.
func oauthFlags(auth *hyperv1.Auth) ([]string, error) {
	spec := auth.Spec
	groupTeamMap := ""
	if len(spec.GroupTeamMap) > 0 {
		encoded, err := json.Marshal(spec.GroupTeamMap)
		if err != nil {
			return nil, fmt.Errorf("encoding groupTeamMap: %w", err)
		}
		groupTeamMap = string(encoded)
	}
	flags := []string{
		"--name=" + sourceName(auth),
		"--provider=" + spec.Provider,
		"--auto-discover-url=" + spec.AutoDiscoveryURL,
		"--icon-url=" + spec.IconURL,
		"--skip-local-2fa=" + strconv.FormatBool(spec.SkipLocal2FA),
		"--required-claim-name=" + spec.RequiredClaimName,
		"--required-claim-value=" + spec.RequiredClaimValue,
		"--group-claim-name=" + spec.GroupClaimName,
		"--admin-group=" + spec.AdminGroup,
		"--restricted-group=" + spec.RestrictedGroup,
		"--group-team-map=" + groupTeamMap,
		"--group-team-map-removal=" + strconv.FormatBool(spec.GroupTeamMapRemoval),
		"--ssh-public-key-claim-name=" + spec.SSHPublicKeyClaimName,
		"--full-name-claim-name=" + spec.FullNameClaimName,
	}
	for _, scope := range spec.Scopes {
		flags = append(flags, "--scopes="+scope)
	}
	if urls := spec.CustomURLs; urls != nil {
		flags = append(flags,
			"--use-custom-urls=true",
			"--custom-auth-url="+urls.AuthURL,
			"--custom-token-url="+urls.TokenURL,
			"--custom-profile-url="+urls.ProfileURL,
			"--custom-email-url="+urls.EmailURL,
			"--custom-tenant-id="+urls.TenantID,
		)
	}
	return flags, nil
}

func configHash(flags []string, clientID, clientSecret string) string {
	sum := sha256.New()
	for _, part := range append(flags, clientID, clientSecret) {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func sourceName(auth *hyperv1.Auth) string {
	if auth.Spec.Name != "" {
		return auth.Spec.Name
	}
	return auth.Name
}

// runGitea runs the gitea CLI in the pod. Credentials travel over stdin so
// they never appear in the exec request or the container's process list.
func (r *AuthReconciler) runGitea(ctx context.Context, pod *corev1.Pod, args []string, clientID, clientSecret string) (string, error) {
	withCredentials := "0"
	if clientID != "" || clientSecret != "" {
		withCredentials = "1"
	}
	script := "WITH_CREDENTIALS=" + withCredentials + "\n" + giteaCLIWrapper
	command := append([]string{"/bin/sh", "-c", script, Gitea}, args...)
	stdin := strings.NewReader(clientID + "\n" + clientSecret + "\n")
	return r.Executor.Exec(ctx, pod.Namespace, pod.Name, Gitea, command, stdin)
}

// authCommand returns the arguments for `gitea admin auth <subcommand> <args>`.
func authCommand(subcommand string, args ...string) []string {
	return append([]string{"admin", "auth", subcommand}, args...)
}

// findSource returns the source with the given name, or nil when absent.
func (r *AuthReconciler) findSource(ctx context.Context, pod *corev1.Pod, name string) (*giteaAuthSource, error) {
	out, err := r.runGitea(ctx, pod, authCommand("list"), "", "")
	if err != nil {
		return nil, fmt.Errorf("listing authentication sources: %w", err)
	}
	sources, err := parseAuthList(out)
	if err != nil {
		return nil, err
	}
	for i := range sources {
		if sources[i].Name == name {
			return &sources[i], nil
		}
	}
	return nil, nil
}

// parseAuthList parses the tab-separated output of `gitea admin auth list`
// (columns: ID, Name, Type, Enabled).
func parseAuthList(out string) ([]giteaAuthSource, error) {
	var sources []giteaAuthSource
	for _, line := range strings.Split(out, "\n") {
		fields := nonEmpty(strings.Split(line, "\t"))
		if len(fields) < 3 || fields[0] == "ID" {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing auth list line %q: %w", line, err)
		}
		sources = append(sources, giteaAuthSource{
			ID:   id,
			Name: strings.TrimSpace(fields[1]),
			Type: strings.TrimSpace(fields[2]),
		})
	}
	return sources, nil
}

func nonEmpty(parts []string) []string {
	kept := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return kept
}

func (r *AuthReconciler) giteaInstance(ctx context.Context, auth *hyperv1.Auth) (*hyperv1.Gitea, error) {
	namespace := auth.Spec.Instance.Namespace
	if namespace == "" {
		namespace = auth.Namespace
	}
	gitea := &hyperv1.Gitea{}
	key := types.NamespacedName{Name: auth.Spec.Instance.Name, Namespace: namespace}
	if err := r.Get(ctx, key, gitea); err != nil {
		return nil, fmt.Errorf("getting gitea instance %s: %w", key, err)
	}
	return gitea, nil
}

// giteaPod returns a ready pod of the Gitea instance's StatefulSet.
func (r *AuthReconciler) giteaPod(ctx context.Context, gitea *hyperv1.Gitea) (*corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(gitea.Namespace), client.MatchingLabels{
		"app.kubernetes.io/name":      Gitea,
		"app.kubernetes.io/component": "deployment",
		"app.kubernetes.io/instance":  gitea.Name,
	}); err != nil {
		return nil, fmt.Errorf("listing gitea pods: %w", err)
	}
	for i := range pods.Items {
		for _, cond := range pods.Items[i].Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return &pods.Items[i], nil
			}
		}
	}
	return nil, errNoGiteaPod
}

func (r *AuthReconciler) secretValue(ctx context.Context, namespace string, selector corev1.SecretKeySelector) (string, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: selector.Name, Namespace: namespace}, secret); err != nil {
		return "", fmt.Errorf("getting secret %s/%s: %w", namespace, selector.Name, err)
	}
	value, ok := secret.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s has no key %q", namespace, selector.Name, selector.Key)
	}
	return strings.TrimSpace(string(value)), nil
}

// setReady records the Ready condition, writing status only when it changed.
// Callers that also changed other status fields use applyStatus.
func (r *AuthReconciler) setReady(ctx context.Context, auth *hyperv1.Auth, status metav1.ConditionStatus, reason, message string) error {
	if !setReadyCondition(auth, status, reason, message) {
		return nil
	}
	return r.Status().Update(ctx, auth)
}

func (r *AuthReconciler) applyStatus(ctx context.Context, auth *hyperv1.Auth, status metav1.ConditionStatus, reason, message string) error {
	setReadyCondition(auth, status, reason, message)
	return r.Status().Update(ctx, auth)
}

func setReadyCondition(auth *hyperv1.Auth, status metav1.ConditionStatus, reason, message string) bool {
	return meta.SetStatusCondition(&auth.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: auth.Generation,
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *AuthReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hyperv1.Auth{}).
		Complete(r)
}
