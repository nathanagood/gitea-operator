package controller

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeGitea stands in for the gitea CLI. It records each invocation's
// arguments, one per line, separated by a "--" line, and prints an admin user
// table that contains the user named in FAKE_EXISTING_ADMIN, if set.
const fakeGitea = `#!/usr/bin/env bash
{ printf '%s\n' "$@"; echo --; } >> "$FAKE_GITEA_LOG"
if [[ "$1 $2 $3" == "admin user list" ]]; then
	echo "ID   Username   Email   IsActive   IsAdmin   2FA"
	[[ -n "${FAKE_EXISTING_ADMIN:-}" ]] && echo "1    ${FAKE_EXISTING_ADMIN}   a@b.c   true   true   false"
fi
exit 0
`

// runConfigureGiteaScript runs configureGiteaScript against fakeGitea and
// returns the argument lists of every gitea invocation.
func runConfigureGiteaScript(t *testing.T, existingAdmin string) [][]string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gitea"), []byte(fakeGitea), 0o755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(dir, "calls.log")
	cmd := exec.Command("bash", "-c", configureGiteaScript)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GITEA_LOG="+logFile,
		"FAKE_EXISTING_ADMIN="+existingAdmin,
		"GITEA_ADMIN_USERNAME=gitea_admin",
		"GITEA_ADMIN_PASSWORD=pa ss word",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.Split(strings.TrimSuffix(string(raw), "--\n"), "--\n")
	calls := make([][]string, 0, len(blocks))
	for _, block := range blocks {
		calls = append(calls, strings.Split(strings.TrimSuffix(block, "\n"), "\n"))
	}
	return calls
}

func findCall(calls [][]string, prefix ...string) []string {
	for _, c := range calls {
		if len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix) {
			return c
		}
	}
	return nil
}

func TestConfigureGiteaScriptResetsExistingAdminPassword(t *testing.T) {
	calls := runConfigureGiteaScript(t, "gitea_admin")

	got := findCall(calls, "admin", "user", "change-password")
	want := []string{
		"admin", "user", "change-password",
		"--username", "gitea_admin",
		"--password", "pa ss word",
		"--must-change-password=false",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("change-password args:\n got  %q\n want %q", got, want)
	}
	if c := findCall(calls, "admin", "user", "create"); c != nil {
		t.Fatalf("unexpected create for an existing admin: %q", c)
	}
}

func TestConfigureGiteaScriptCreatesMissingAdmin(t *testing.T) {
	calls := runConfigureGiteaScript(t, "")

	got := findCall(calls, "admin", "user", "create")
	want := []string{
		"admin", "user", "create", "--admin",
		"--username", "gitea_admin",
		"--password", "pa ss word",
		"--email", "gitea@local.domain",
		"--must-change-password=false",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("create args:\n got  %q\n want %q", got, want)
	}
}
