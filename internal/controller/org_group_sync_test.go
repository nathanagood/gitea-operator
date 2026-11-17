package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

func TestEffectiveMemberPolicy(t *testing.T) {
	org := &hyperv1.Org{
		ObjectMeta: metav1.ObjectMeta{Name: "itz", Namespace: "gitea"},
		Spec:       hyperv1.OrgSpec{Instance: hyperv1.InstanceType{Name: "git-techzone"}},
	}
	now := metav1.Now()
	auths := []hyperv1.Auth{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "verify", Namespace: "gitea"},
			Spec: hyperv1.AuthSpec{
				Instance: hyperv1.InstanceType{Name: "git-techzone"},
				GroupTeamMap: hyperv1.GroupTeamMap{
					"ITZ_ACL_development": {"ITZ": {"Development"}, "itz-content": {"Support"}},
				},
			},
		},
		{ // different instance: ignored
			ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "other"},
			Spec: hyperv1.AuthSpec{
				Instance:     hyperv1.InstanceType{Name: "git-techzone"},
				GroupTeamMap: hyperv1.GroupTeamMap{"g": {"itz": {"Security"}}},
			},
		},
		{ // being deleted: ignored
			ObjectMeta: metav1.ObjectMeta{Name: "gone", Namespace: "gitea", DeletionTimestamp: &now},
			Spec: hyperv1.AuthSpec{
				Instance:     hyperv1.InstanceType{Name: "git-techzone", Namespace: "gitea"},
				GroupTeamMap: hyperv1.GroupTeamMap{"g": {"itz": {"Offering"}}},
			},
		},
	}
	mapped := teamsMappedFromAuths(org, auths)

	cases := []struct {
		team hyperv1.Team
		want hyperv1.TeamMemberPolicy
	}{
		{hyperv1.Team{Name: "development"}, hyperv1.TeamMemberPolicyIgnore},
		{hyperv1.Team{Name: "Support"}, hyperv1.TeamMemberPolicyExact},  // mapped in another org
		{hyperv1.Team{Name: "Security"}, hyperv1.TeamMemberPolicyExact}, // other instance
		{hyperv1.Team{Name: "Offering"}, hyperv1.TeamMemberPolicyExact}, // deleted auth
		{hyperv1.Team{Name: "Development", MemberPolicy: hyperv1.TeamMemberPolicyAdditive}, hyperv1.TeamMemberPolicyAdditive},
	}
	for _, c := range cases {
		if got := effectiveMemberPolicy(&c.team, mapped); got != c.want {
			t.Errorf("team %q policy %q: got %s, want %s", c.team.Name, c.team.MemberPolicy, got, c.want)
		}
	}
}
