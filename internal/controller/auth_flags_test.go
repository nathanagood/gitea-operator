package controller

import (
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
)

func TestParseAuthList(t *testing.T) {
	out := "ID\tName\t\t\t\tType\tEnabled\n" +
		"1\tw3id\t\t\t\tOAuth2\ttrue\n" +
		"2\tIBM Security Verify\tOAuth2\ttrue\n"
	sources, err := parseAuthList(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("want 2 sources, got %d: %+v", len(sources), sources)
	}
	if sources[1].ID != 2 || sources[1].Name != "IBM Security Verify" || sources[1].Type != "OAuth2" {
		t.Errorf("unexpected source: %+v", sources[1])
	}
}

func TestOauthFlags(t *testing.T) {
	auth := &hyperv1.Auth{
		ObjectMeta: metav1.ObjectMeta{Name: "ibm-security-verify"},
		Spec: hyperv1.AuthSpec{
			Name:           "IBM Security Verify",
			Provider:       "openidConnect",
			Scopes:         []string{"openid", "email"},
			GroupClaimName: "blue_groups",
			GroupTeamMap: hyperv1.GroupTeamMap{
				"itz-dev": {"ITZ": {"Development"}},
			},
		},
	}
	flags, err := oauthFlags(auth)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--name=IBM Security Verify",
		"--provider=openidConnect",
		"--group-claim-name=blue_groups",
		`--group-team-map={"itz-dev":{"ITZ":["Development"]}}`,
		"--group-team-map-removal=false",
		"--admin-group=",
		"--scopes=openid",
		"--scopes=email",
	} {
		if !slices.Contains(flags, want) {
			t.Errorf("missing flag %q in %v", want, flags)
		}
	}
	if slices.ContainsFunc(flags, func(f string) bool { return f == "--use-custom-urls=true" }) {
		t.Error("custom URLs must not be enabled without spec.customURLs")
	}
}

func TestConfigHashChangesWithSecret(t *testing.T) {
	flags := []string{"--name=x"}
	if configHash(flags, "id", "a") == configHash(flags, "id", "b") {
		t.Error("hash must change when the client secret changes")
	}
}
