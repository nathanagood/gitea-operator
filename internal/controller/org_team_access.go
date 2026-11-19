package controller

import (
	"maps"
	"slices"

	g "code.gitea.io/sdk/gitea"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

// allTeamUnits is every repository unit a team can be given.
var allTeamUnits = []g.RepoUnitType{
	g.RepoUnitCode,
	g.RepoUnitIssues,
	g.RepoUnitPulls,
	g.RepoUnitExtIssues,
	g.RepoUnitWiki,
	g.RepoUnitExtWiki,
	g.RepoUnitReleases,
	g.RepoUnitProjects,
	g.RepoUnitPackages,
	g.RepoUnitActions,
}

// teamAccess is the permission part of a Gitea create/edit team request.
type teamAccess struct {
	Permission g.AccessMode
	Units      []g.RepoUnitType
	UnitsMap   map[string]string
}

// teamAccessFor turns a Team spec into the request Gitea expects. Gitea gives
// admin and owner teams admin access to every unit. For other teams it uses
// units_map when present, otherwise applies `permission` (read or write) to
// `units`; it rejects a team with neither. The SDK also rejects permission
// "none", so per-unit teams send read, which Gitea ignores in favour of the map.
func teamAccessFor(team *hyperv1.Team) teamAccess {
	switch team.Permission {
	case "owner":
		return teamAccess{Permission: g.AccessModeOwner, Units: allTeamUnits}
	case "admin":
		return teamAccess{Permission: g.AccessModeAdmin, Units: allTeamUnits}
	}

	if len(team.UnitsMap) > 0 {
		keys := slices.Sorted(maps.Keys(team.UnitsMap))
		units := make([]g.RepoUnitType, len(keys))
		for i, k := range keys {
			units[i] = g.RepoUnitType(k)
		}
		unitsMap := make(map[string]string, len(team.UnitsMap))
		for k, v := range team.UnitsMap {
			unitsMap[k] = string(v)
		}
		return teamAccess{Permission: g.AccessModeRead, Units: units, UnitsMap: unitsMap}
	}

	perm := g.AccessModeRead
	if team.Permission == "write" {
		perm = g.AccessModeWrite
	}
	units := allTeamUnits
	if len(team.Units) > 0 {
		units = make([]g.RepoUnitType, len(team.Units))
		for i, u := range team.Units {
			units[i] = g.RepoUnitType(u)
		}
	}
	return teamAccess{Permission: perm, Units: units}
}
