package git

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type Credentials struct {
	Username      string
	Password      string
	Token         string
	SSHPrivateKey []byte
	KnownHosts    []byte
}

type Repository struct {
	CacheDir string
}

var sha1Re = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
var sshSCPStyle = regexp.MustCompile(`^[^@[:space:]]+@[^:[:space:]]+:.+`)

type gitAuth struct {
	args    []string
	env     []string
	cleanup func()
}

func (r Repository) Checkout(ctx context.Context, repoURL, revision string, creds *Credentials) (string, string, error) {
	if repoURL == "" {
		return "", "", fmt.Errorf("repository URL is required")
	}
	if revision == "" {
		revision = "main"
	}
	base := r.cacheRoot()
	repoKey := repoHash(repoURL)
	mirrorDir := filepath.Join(base, "mirrors", repoKey+".git")
	checkoutDir := filepath.Join(base, "checkouts", repoKey)
	if err := os.MkdirAll(filepath.Dir(mirrorDir), 0o755); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(checkoutDir, 0o755); err != nil {
		return "", "", err
	}
	auth, err := r.prepareAuth(base, repoURL, creds)
	if err != nil {
		return "", "", err
	}
	if auth != nil && auth.cleanup != nil {
		defer auth.cleanup()
	}
	if err := r.withLock(mirrorDir+".lockfile", 30*time.Second, func() error {
		if err := r.ensureMirror(ctx, mirrorDir, repoURL, auth); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return "", "", err
	}
	sha, err := resolveRevision(ctx, mirrorDir, revision, auth)
	if err != nil {
		return "", "", err
	}
	finalDir := filepath.Join(checkoutDir, sha)
	if st, err := os.Stat(finalDir); err == nil && st.IsDir() {
		return finalDir, sha, nil
	}
	if err := r.withLock(finalDir+".lockfile", 30*time.Second, func() error {
		if st, err := os.Stat(finalDir); err == nil && st.IsDir() {
			return nil
		}
		tempDir, err := os.MkdirTemp(checkoutDir, "tmp-*")
		if err != nil {
			return err
		}
		cleanupDir := tempDir
		defer func() {
			if cleanupDir != "" {
				_ = os.RemoveAll(cleanupDir)
			}
		}()
		if err := runGit(ctx, mirrorDir, auth, "clone", "--no-checkout", mirrorDir, tempDir); err != nil {
			return err
		}
		if err := runGit(ctx, tempDir, auth, "checkout", "--force", "--detach", sha); err != nil {
			return err
		}
		if err := os.Rename(tempDir, finalDir); err != nil {
			if errors.Is(err, os.ErrExist) {
				cleanupDir = ""
				return nil
			}
			if _, statErr := os.Stat(finalDir); statErr == nil {
				cleanupDir = ""
				return nil
			}
			return err
		}
		cleanupDir = ""
		return nil
	}); err != nil {
		return "", "", err
	}
	return finalDir, sha, nil
}

func (r Repository) ensureMirror(ctx context.Context, mirrorDir, repoURL string, auth *gitAuth) error {
	return r.ensureMirrorAttempt(ctx, mirrorDir, repoURL, auth, false)
}

func (r Repository) ensureMirrorAttempt(ctx context.Context, mirrorDir, repoURL string, auth *gitAuth, recovered bool) error {
	if _, err := os.Stat(filepath.Join(mirrorDir, "HEAD")); os.IsNotExist(err) {
		if err := runGit(ctx, "", auth, "init", "--bare", mirrorDir); err != nil {
			if !recovered && isConfigCorruptionError(err) {
				if rmErr := os.RemoveAll(mirrorDir); rmErr != nil {
					return rmErr
				}
				return r.ensureMirrorAttempt(ctx, mirrorDir, repoURL, auth, true)
			}
			return err
		}
	}
	_ = runGit(ctx, mirrorDir, auth, "remote", "remove", "origin")
	if err := runGit(ctx, mirrorDir, auth, "remote", "add", "origin", repoURL); err != nil {
		if !recovered && isConfigCorruptionError(err) {
			if err := os.RemoveAll(mirrorDir); err != nil {
				return err
			}
			return r.ensureMirrorAttempt(ctx, mirrorDir, repoURL, auth, true)
		}
		if !strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	if err := runGit(ctx, mirrorDir, auth, "fetch", "--prune", "--tags", "--force", "--prune-tags", "origin", "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"); err != nil {
		if !recovered && isConfigCorruptionError(err) {
			if err := os.RemoveAll(mirrorDir); err != nil {
				return err
			}
			return r.ensureMirrorAttempt(ctx, mirrorDir, repoURL, auth, true)
		}
		return err
	}
	return nil
}

func resolveRevision(ctx context.Context, mirrorDir, revision string, auth *gitAuth) (string, error) {
	if sha1Re.MatchString(revision) {
		if err := runGit(ctx, mirrorDir, auth, "rev-parse", "--verify", revision+"^{commit}"); err != nil {
			return "", fmt.Errorf("verify commit %s: %w", revision, err)
		}
		return strings.ToLower(revision), nil
	}
	out, err := runGitOutput(ctx, mirrorDir, auth, "ls-remote", "--refs", "origin", revision)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("revision %q not found", revision)
	}
	sha := fields[0]
	if !sha1Re.MatchString(sha) {
		return "", fmt.Errorf("unexpected revision response for %q", revision)
	}
	return sha, nil
}

func runGit(ctx context.Context, dir string, auth *gitAuth, args ...string) error {
	_, err := runGitCommand(ctx, dir, auth, args...)
	return err
}

func runGitOutput(ctx context.Context, dir string, auth *gitAuth, args ...string) (string, error) {
	return runGitCommand(ctx, dir, auth, args...)
}

func runGitCommand(ctx context.Context, dir string, auth *gitAuth, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, gitBinaryPath(), args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=true",
	)
	if auth != nil && len(auth.env) > 0 {
		cmd.Env = append(cmd.Env, auth.env...)
	}
	if auth != nil && len(auth.args) > 0 {
		cmd.Args = append([]string{"git"}, append(auth.args, args...)...)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, redact(string(output)))
	}
	return string(output), nil
}

func (r Repository) prepareAuth(base, repoURL string, creds *Credentials) (*gitAuth, error) {
	if isSSHRepositoryURL(repoURL) {
		if creds == nil || len(creds.SSHPrivateKey) == 0 || len(creds.KnownHosts) == 0 {
			return nil, fmt.Errorf("SSH repository requires ssh-privatekey and known_hosts credentials")
		}
		authRoot := filepath.Join(base, "auth")
		if err := os.MkdirAll(authRoot, 0o755); err != nil {
			return nil, err
		}
		authDir, err := os.MkdirTemp(authRoot, "ssh-*")
		if err != nil {
			return nil, err
		}
		privateKeyPath := filepath.Join(authDir, "id_ed25519")
		knownHostsPath := filepath.Join(authDir, "known_hosts")
		if err := os.WriteFile(privateKeyPath, creds.SSHPrivateKey, 0o600); err != nil {
			os.RemoveAll(authDir)
			return nil, err
		}
		if err := os.WriteFile(knownHostsPath, creds.KnownHosts, 0o600); err != nil {
			os.RemoveAll(authDir)
			return nil, err
		}
		return &gitAuth{
			env: []string{
				"GIT_SSH_COMMAND=" + sshBinaryPath() + " -F /dev/null -i " + shellQuote(privateKeyPath) + " -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=" + shellQuote(knownHostsPath),
			},
			cleanup: func() {
				_ = os.RemoveAll(authDir)
			},
		}, nil
	}
	if header := authHeader(creds); header != "" {
		return &gitAuth{args: []string{"-c", "http.extraHeader=Authorization: Basic " + header}}, nil
	}
	return nil, nil
}

func authHeader(creds *Credentials) string {
	if creds == nil {
		return ""
	}
	username := creds.Username
	password := creds.Password
	if password == "" && creds.Token != "" {
		password = creds.Token
	}
	if username == "" && password == "" {
		return ""
	}
	if username == "" {
		username = "x-access-token"
	}
	token := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return token
}

func shellQuote(value string) string {
	return fmt.Sprintf("%q", value)
}

func gitBinaryPath() string {
	return firstExistingBinary("git", "/usr/bin/git", "/bin/git")
}

func sshBinaryPath() string {
	return firstExistingBinary("ssh", "/usr/bin/ssh", "/bin/ssh")
}

func firstExistingBinary(name string, candidates ...string) string {
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return name
}

func isSSHRepositoryURL(repoURL string) bool {
	return strings.HasPrefix(repoURL, "ssh://") || sshSCPStyle.MatchString(repoURL)
}

func redact(s string) string {
	return s
}

func repoHash(repoURL string) string {
	sum := sha256.Sum256([]byte(repoURL))
	return hex.EncodeToString(sum[:])
}

func (r Repository) cacheRoot() string {
	if r.CacheDir != "" {
		return r.CacheDir
	}
	return filepath.Join(os.TempDir(), "cabure-cache")
}

func (r Repository) withLock(path string, timeout time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			defer func() {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
			}()
			return fn()
		} else {
			_ = file.Close()
			if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
				return err
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for lock %s", path)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func isGitRemoteMissing(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "remote origin") ||
		strings.Contains(msg, "No such remote") ||
		strings.Contains(msg, "does not have a remote called")
}

func isConfigCorruptionError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "bad config line") ||
		strings.Contains(msg, "unable to read config file") ||
		strings.Contains(msg, "could not read config file") ||
		strings.Contains(msg, "error reading config file")
}
