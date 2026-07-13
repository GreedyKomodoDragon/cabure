package git

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestIsSSHRepositoryURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
		want bool
	}{
		{name: "https", url: "https://example.com/org/repo.git", want: false},
		{name: "ssh scheme", url: "ssh://git@example.com/org/repo.git", want: true},
		{name: "scp style", url: "git@example.com:org/repo.git", want: true},
		{name: "invalid", url: "git@example.com/org/repo.git", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isSSHRepositoryURL(tc.url); got != tc.want {
				t.Fatalf("isSSHRepositoryURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestPrepareAuthHTTPS(t *testing.T) {
	t.Parallel()

	auth, err := Repository{}.prepareAuth(t.TempDir(), "https://example.com/org/repo.git", &Credentials{
		Username: "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("prepareAuth returned error: %v", err)
	}
	if auth == nil {
		t.Fatal("expected auth session")
	}
	if got, want := len(auth.args), 2; got != want {
		t.Fatalf("len(auth.args) = %d, want %d", got, want)
	}
	if auth.args[0] != "-c" {
		t.Fatalf("auth.args[0] = %q, want -c", auth.args[0])
	}
	wantHeader := "http.extraHeader=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if !strings.Contains(auth.args[1], wantHeader) {
		t.Fatalf("auth.args[1] = %q, want to contain %q", auth.args[1], wantHeader)
	}
	if len(auth.env) != 0 {
		t.Fatalf("expected no env overrides for HTTPS auth, got %v", auth.env)
	}
}

func TestPrepareAuthSSH(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	auth, err := Repository{}.prepareAuth(base, "git@example.com:org/repo.git", &Credentials{
		SSHPrivateKey: []byte("private-key\n"),
		KnownHosts:    []byte("example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample\n"),
	})
	if err != nil {
		t.Fatalf("prepareAuth returned error: %v", err)
	}
	if auth == nil {
		t.Fatal("expected auth session")
	}
	if auth.cleanup != nil {
		t.Cleanup(auth.cleanup)
	}
	if len(auth.args) != 0 {
		t.Fatalf("expected no extra git args for SSH auth, got %v", auth.args)
	}
	if len(auth.env) != 1 {
		t.Fatalf("expected one env override for SSH auth, got %v", auth.env)
	}
	command := strings.TrimPrefix(auth.env[0], "GIT_SSH_COMMAND=")
	if !strings.Contains(command, "StrictHostKeyChecking=yes") {
		t.Fatalf("GIT_SSH_COMMAND missing strict host key checking: %q", command)
	}
	if !strings.Contains(command, "UserKnownHostsFile=") {
		t.Fatalf("GIT_SSH_COMMAND missing known hosts path: %q", command)
	}
	keyPath := extractQuotedPath(t, command, `-i`)
	knownHostsPath := extractQuotedPath(t, command, `UserKnownHostsFile=`)
	if got, err := os.ReadFile(keyPath); err != nil {
		t.Fatalf("read private key: %v", err)
	} else if string(got) != "private-key\n" {
		t.Fatalf("private key contents = %q, want %q", got, "private-key\n")
	}
	if got, err := os.ReadFile(knownHostsPath); err != nil {
		t.Fatalf("read known_hosts: %v", err)
	} else if string(got) != "example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample\n" {
		t.Fatalf("known_hosts contents = %q, want expected known_hosts", got)
	}
}

func TestPrepareAuthSSHRejectsMissingKnownHosts(t *testing.T) {
	t.Parallel()

	_, err := Repository{}.prepareAuth(t.TempDir(), "git@example.com:org/repo.git", &Credentials{
		SSHPrivateKey: []byte("private-key\n"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckoutLeavesMaterializedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	remoteDir := filepath.Join(base, "remote.git")
	workDir := filepath.Join(base, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Cabure",
			"GIT_AUTHOR_EMAIL=cabure@example.com",
			"GIT_COMMITTER_NAME=Cabure",
			"GIT_COMMITTER_EMAIL=cabure@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run(base, "init", "--bare", remoteDir)
	run(workDir, "init")
	run(workDir, "branch", "-M", "main")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run(workDir, "add", "README.md")
	run(workDir, "commit", "-m", "initial")
	run(workDir, "remote", "add", "origin", remoteDir)
	run(workDir, "push", "-u", "origin", "main")

	cacheDir := filepath.Join(base, "cache")
	cloneDir, sha, err := Repository{CacheDir: cacheDir}.Checkout(context.Background(), remoteDir, "main", nil)
	if err != nil {
		t.Fatalf("Checkout returned error: %v", err)
	}
	if cloneDir == "" || sha == "" {
		t.Fatalf("expected cloneDir and sha, got %q %q", cloneDir, sha)
	}
	if _, err := os.Stat(filepath.Join(cloneDir, "README.md")); err != nil {
		t.Fatalf("expected checkout contents to remain materialized: %v", err)
	}
}

func TestCheckoutRebuildsCorruptedMirror(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	remoteDir := filepath.Join(base, "remote.git")
	workDir := filepath.Join(base, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Cabure",
			"GIT_AUTHOR_EMAIL=cabure@example.com",
			"GIT_COMMITTER_NAME=Cabure",
			"GIT_COMMITTER_EMAIL=cabure@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run(base, "init", "--bare", remoteDir)
	run(workDir, "init")
	run(workDir, "branch", "-M", "main")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run(workDir, "add", "README.md")
	run(workDir, "commit", "-m", "initial")
	run(workDir, "remote", "add", "origin", remoteDir)
	run(workDir, "push", "-u", "origin", "main")

	cacheDir := filepath.Join(base, "cache")
	repoKey := repoHash(remoteDir)
	mirrorDir := filepath.Join(cacheDir, "mirrors", repoKey+".git")
	if err := os.MkdirAll(mirrorDir, 0o755); err != nil {
		t.Fatalf("mkdir mirror: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mirrorDir, "config"), []byte("bad config line 1\n"), 0o644); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}

	cloneDir, sha, err := Repository{CacheDir: cacheDir}.Checkout(context.Background(), remoteDir, "main", nil)
	if err != nil {
		t.Fatalf("Checkout returned error: %v", err)
	}
	if cloneDir == "" || sha == "" {
		t.Fatalf("expected cloneDir and sha, got %q %q", cloneDir, sha)
	}
	if _, err := os.Stat(filepath.Join(cloneDir, "README.md")); err != nil {
		t.Fatalf("expected checkout contents to remain materialized: %v", err)
	}
}

func extractQuotedPath(t *testing.T, command, prefix string) string {
	t.Helper()

	re := regexp.MustCompile(regexp.QuoteMeta(prefix) + `\s*"([^"]+)"`)
	matches := re.FindStringSubmatch(command)
	if len(matches) != 2 {
		t.Fatalf("could not find %s path in %q", prefix, command)
	}
	return matches[1]
}
