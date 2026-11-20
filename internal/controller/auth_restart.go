package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

// authAnnotationPrefix marks the Gitea pod template with the config hash of
// each applied Auth.
//
// `gitea admin auth add-oauth|update-oauth|delete` only changes the database:
// the CLI process registers the OAuth2 provider in its own memory and exits,
// and the running server keeps the providers it loaded at startup. Changing the
// annotation rolls the StatefulSet so the server reloads them. Writing the same
// hash again is a no-op, so re-applying an unchanged config never restarts.
const authAnnotationPrefix = "auth.hyperspike.io/"

// authAnnotationKey is the pod template annotation for one Auth. The name part
// of an annotation key is limited to 63 characters.
func authAnnotationKey(auth *hyperv1.Auth) string {
	name := auth.Namespace + "." + auth.Name
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-.")
	}
	return authAnnotationPrefix + name
}

// restartGiteaForAuth sets the Auth's pod template annotation to hash, or
// removes it when hash is empty. The StatefulSet rolls only if the value
// changed.
func (r *AuthReconciler) restartGiteaForAuth(ctx context.Context, gitea *hyperv1.Gitea, auth *hyperv1.Auth, hash string) error {
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: gitea.Namespace, Name: gitea.Name}, sts); err != nil {
		return fmt.Errorf("getting gitea statefulset: %w", err)
	}
	key := authAnnotationKey(auth)
	current, present := sts.Spec.Template.Annotations[key]
	if current == hash && (present || hash == "") {
		return nil
	}

	var value any = hash // a null value removes the annotation in a merge patch
	if hash == "" {
		value = nil
	}
	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{
			"annotations": map[string]any{key: value},
		}}},
	})
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("restarting gitea to load authentication sources", "statefulset", gitea.Name, "annotation", key)
	if err := r.Patch(ctx, sts, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return fmt.Errorf("restarting gitea statefulset: %w", err)
	}
	return nil
}
