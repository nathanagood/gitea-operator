package controller

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

// groupMappedTeams is the set of team names (lowercased) in one org that an
// Auth groupTeamMap fills. Gitea matches org and team names case-insensitively.
type groupMappedTeams map[string]bool

// instanceKey resolves an instance reference, which defaults to the
// referencing object's namespace.
func instanceKey(instance hyperv1.InstanceType, defaultNamespace string) types.NamespacedName {
	ns := instance.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	return types.NamespacedName{Namespace: ns, Name: instance.Name}
}

// teamsMappedFromAuths returns the teams of org that any live Auth for the
// same Gitea instance maps a group to.
func teamsMappedFromAuths(org *hyperv1.Org, auths []hyperv1.Auth) groupMappedTeams {
	mapped := groupMappedTeams{}
	orgInstance := instanceKey(org.Spec.Instance, org.Namespace)
	for i := range auths {
		auth := &auths[i]
		if auth.DeletionTimestamp != nil || instanceKey(auth.Spec.Instance, auth.Namespace) != orgInstance {
			continue
		}
		for _, orgTeams := range auth.Spec.GroupTeamMap {
			for orgName, teams := range orgTeams {
				if !strings.EqualFold(orgName, org.Name) {
					continue
				}
				for _, team := range teams {
					mapped[strings.ToLower(team)] = true
				}
			}
		}
	}
	return mapped
}

// effectiveMemberPolicy applies the default for an unset memberPolicy: the
// identity provider owns membership of group-mapped teams.
func effectiveMemberPolicy(team *hyperv1.Team, mapped groupMappedTeams) hyperv1.TeamMemberPolicy {
	if team.MemberPolicy != "" {
		return team.MemberPolicy
	}
	if mapped[strings.ToLower(team.Name)] {
		return hyperv1.TeamMemberPolicyIgnore
	}
	return hyperv1.TeamMemberPolicyExact
}

func (r *OrgReconciler) listGroupMappedTeams(ctx context.Context, org *hyperv1.Org) (groupMappedTeams, error) {
	auths := &hyperv1.AuthList{}
	if err := r.List(ctx, auths); err != nil {
		return nil, fmt.Errorf("listing auths: %w", err)
	}
	return teamsMappedFromAuths(org, auths.Items), nil
}

// orgsForAuth requeues every Org on the Auth's Gitea instance, so a change to
// groupTeamMap updates which teams the operator leaves alone.
func (r *OrgReconciler) orgsForAuth(ctx context.Context, obj client.Object) []reconcile.Request {
	auth, ok := obj.(*hyperv1.Auth)
	if !ok {
		return nil
	}
	orgs := &hyperv1.OrgList{}
	if err := r.List(ctx, orgs); err != nil {
		log.FromContext(ctx).Error(err, "listing orgs for auth", "auth", client.ObjectKeyFromObject(auth))
		return nil
	}
	authInstance := instanceKey(auth.Spec.Instance, auth.Namespace)
	var requests []reconcile.Request
	for i := range orgs.Items {
		org := &orgs.Items[i]
		if instanceKey(org.Spec.Instance, org.Namespace) == authInstance {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(org)})
		}
	}
	return requests
}
