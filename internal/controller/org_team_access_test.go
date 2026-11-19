package controller

import (
	"slices"
	"testing"

	g "code.gitea.io/sdk/gitea"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

func TestTeamAccessFor(t *testing.T) {
	cases := []struct {
		name     string
		team     hyperv1.Team
		perm     g.AccessMode
		units    []g.RepoUnitType
		unitsMap map[string]string
	}{
		{"owner", hyperv1.Team{Permission: "owner", Units: []string{"repo.code"}}, g.AccessModeOwner, allTeamUnits, nil},
		{"admin ignores map", hyperv1.Team{Permission: "admin", UnitsMap: map[string]hyperv1.TeamUnitAccess{"repo.code": "read"}}, g.AccessModeAdmin, allTeamUnits, nil},
		{"unset is read on all units", hyperv1.Team{}, g.AccessModeRead, allTeamUnits, nil},
		{"none is read", hyperv1.Team{Permission: "none", Units: []string{"repo.code"}}, g.AccessModeRead, []g.RepoUnitType{g.RepoUnitCode}, nil},
		{"write on listed units", hyperv1.Team{Permission: "write", Units: []string{"repo.code", "repo.wiki"}}, g.AccessModeWrite, []g.RepoUnitType{g.RepoUnitCode, g.RepoUnitWiki}, nil},
		{
			"units map wins", hyperv1.Team{Permission: "write", Units: []string{"repo.actions"}, UnitsMap: map[string]hyperv1.TeamUnitAccess{"repo.wiki": "read", "repo.code": "write"}},
			g.AccessModeRead, []g.RepoUnitType{g.RepoUnitCode, g.RepoUnitWiki}, map[string]string{"repo.code": "write", "repo.wiki": "read"},
		},
	}
	for _, c := range cases {
		got := teamAccessFor(&c.team)
		if got.Permission != c.perm || !slices.Equal(got.Units, c.units) || len(got.UnitsMap) != len(c.unitsMap) {
			t.Errorf("%s: got %+v", c.name, got)
			continue
		}
		for k, v := range c.unitsMap {
			if got.UnitsMap[k] != v {
				t.Errorf("%s: unitsMap[%s] = %q, want %q", c.name, k, got.UnitsMap[k], v)
			}
		}
	}
}
