// Package builder integrates the optional ONLYOFFICE Document Builder
// binary. The backend is strictly optional: when no binary is detected the
// Builder reports Available()==false and every operation fails with
// ErrUnavailable, so the core never degrades because of it.
//
// Trust boundary: the builder binary itself is trusted and this package
// cannot sandbox a hostile binary. The containment below only limits
// accidents: argv-only exec (no shell), a minimal environment, a throwaway
// working directory, path containment inside the allowed roots, and
// input/output size caps.
//
// Process-group caveat: on timeout the whole process group is killed with
// kill(-pid, SIGKILL) via Setpgid, which is Unix-only (no Windows equivalent
// in this file). The server targets Linux; a Windows port needs a
// build-tagged alternative for execute().
package builder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/storage"
)

// Limits and defaults.
const (
	// DefaultTimeout caps Convert/Render calls.
	DefaultTimeout = 60 * time.Second
	// versionTimeout caps Version calls.
	versionTimeout = 5 * time.Second
	// MaxInputBytes caps the input file size (512 MiB).
	MaxInputBytes = int64(512 << 20)
	// MaxOutputBytes caps the produced file size (1 GiB hard stop).
	MaxOutputBytes = int64(1 << 30)
	// maxStderrSnippet bounds stderr text passed back to callers.
	maxStderrSnippet = 200
)

// Typed errors returned by this package.
var (
	ErrUnavailable   = errors.New("document builder unavailable")
	ErrTimeout       = errors.New("document builder timed out")
	ErrExit          = errors.New("document builder failed")
	ErrOutputMissing = errors.New("document builder produced no output")
	ErrOutsideRoots  = errors.New("path outside allowed roots")
	ErrTooLarge      = errors.New("file over size limit")
)

// binaryNames are looked up in PATH, in order, when no explicit path is
// configured. Paths are never invented outside this list.
var binaryNames = []string{"documentbuilder", "document-builder", "docbuilder"}

// urlScheme rejects inputs that look like URLs (no network fetching).
var urlScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*://`)

// absPathSeg matches absolute paths so stderr can be scrubbed of them.
var absPathSeg = regexp.MustCompile(`/([^/\s"']+/)+[^/\s"']*`)

// Builder wraps the optional Document Builder binary. SetRoots must be
// called before Convert/Render (server.New does this); afterwards a Builder
// is safe for concurrent use (fields are read-only).
type Builder struct {
	path    string
	timeout time.Duration
	roots   []string
}

// Detect locates the builder binary without ever failing: an explicit
// configured path wins (and a bad one simply means unavailable, never a
// PATH fallback); otherwise each name in binaryNames is looked up in PATH.
// The result reports Available()==false when nothing is found.
func Detect(configured string) *Builder {
	b := &Builder{timeout: DefaultTimeout}
	if strings.TrimSpace(configured) != "" {
		if isUsableFile(configured) {
			b.path = configured
		} else {
			slog.Warn("document builder configured path unusable, backend disabled", "path", configured)
		}
		return b
	}
	for _, name := range binaryNames {
		if p, err := exec.LookPath(name); err == nil {
			b.path = p
			break
		}
	}
	return b
}

func isUsableFile(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return true
}

// SetRoots replaces the allowed roots used for path containment.
func (b *Builder) SetRoots(roots []string) {
	b.roots = append([]string(nil), roots...)
}

// SetTimeout overrides the Convert/Render deadline (tests use a short one).
func (b *Builder) SetTimeout(d time.Duration) {
	b.timeout = d
}

// Available reports whether a binary was detected.
func (b *Builder) Available() bool { return b.path != "" }

// Path returns the detected binary path, or "" when unavailable.
func (b *Builder) Path() string { return b.path }

func (b *Builder) effTimeout() time.Duration {
	if b.timeout <= 0 {
		return DefaultTimeout
	}
	return b.timeout
}

// Version runs "<path> --version" with a 5 s timeout.
func (b *Builder) Version(ctx context.Context) (string, error) {
	if !b.Available() {
		return "", ErrUnavailable
	}
	out, err := b.execute(ctx, versionTimeout, []string{"--version"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Convert runs: <binary> <input> <output>.
func (b *Builder) Convert(ctx context.Context, input, output string) error {
	if !b.Available() {
		return ErrUnavailable
	}
	in, err := b.resolveInput(input)
	if err != nil {
		return err
	}
	out, err := b.resolveOutput(output)
	if err != nil {
		return err
	}
	if _, err := b.execute(ctx, b.effTimeout(), []string{in, out}); err != nil {
		return err
	}
	return b.checkOutput(out)
}

// Render runs: <binary> <input> <format> <output>.
// The positional argv convention is this package's contract with the backend
// shim; if the real Document Builder CLI differs, adapt it here (and only
// here) so callers stay unchanged.
func (b *Builder) Render(ctx context.Context, input, format, output string) error {
	if !b.Available() {
		return ErrUnavailable
	}
	format = strings.TrimSpace(format)
	if format == "" || strings.ContainsAny(format, `/\`) || strings.Contains(format, "..") {
		return fmt.Errorf("invalid format %q", format)
	}
	in, err := b.resolveInput(input)
	if err != nil {
		return err
	}
	out, err := b.resolveOutput(output)
	if err != nil {
		return err
	}
	if _, err := b.execute(ctx, b.effTimeout(), []string{in, format, out}); err != nil {
		return err
	}
	return b.checkOutput(out)
}

// resolveInput validates before any process is spawned: URL-looking paths,
// paths escaping the allowed roots (.., symlink escapes via ResolvePath),
// and non-regular or oversized files are all rejected.
func (b *Builder) resolveInput(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("input is required")
	}
	if urlScheme.MatchString(strings.TrimSpace(p)) {
		return "", fmt.Errorf("%w: refusing URL input", ErrOutsideRoots)
	}
	rp, err := storage.ResolvePath(p, b.roots)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOutsideRoots, err)
	}
	fi, err := os.Lstat(rp)
	if err != nil {
		return "", fmt.Errorf("stat input: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("refusing non-regular input file: %s", rp)
	}
	if fi.Size() > MaxInputBytes {
		return "", fmt.Errorf("%w: input is %d bytes, over the %d byte limit", ErrTooLarge, fi.Size(), MaxInputBytes)
	}
	return rp, nil
}

// resolveOutput requires the destination's parent to sit inside the allowed
// roots (leaf symlinks are never followed) and creates the parent dir.
func (b *Builder) resolveOutput(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("output is required")
	}
	if urlScheme.MatchString(strings.TrimSpace(p)) {
		return "", fmt.Errorf("%w: refusing URL output", ErrOutsideRoots)
	}
	rp, err := storage.ResolveDest(p, b.roots)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOutsideRoots, err)
	}
	if err := os.MkdirAll(filepath.Dir(rp), 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}
	return rp, nil
}

// checkOutput enforces the post-call contract: the file must exist and be
// regular, and files over MaxOutputBytes are removed and reported.
func (b *Builder) checkOutput(out string) error {
	fi, err := os.Lstat(out)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrOutputMissing, out)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file: %s", ErrOutputMissing, out)
	}
	if fi.Size() > MaxOutputBytes {
		_ = os.Remove(out)
		return fmt.Errorf("%w: output is %d bytes, over the %d byte limit", ErrTooLarge, fi.Size(), MaxOutputBytes)
	}
	return nil
}

// execute runs argv with no shell and no string-built command line: the
// binary path plus an argv slice only. The child gets a minimal environment
// (allowlist PATH/LANG/TMPDIR plus HOME pointed at a per-call temp dir that
// is also the working directory), never the parent environment. The temp
// dir is removed after the call even on failure.
func (b *Builder) execute(ctx context.Context, timeout time.Duration, argv []string) (string, error) {
	workdir, err := os.MkdirTemp("", "mcp-onlyoffice-builder-*")
	if err != nil {
		return "", fmt.Errorf("create builder workdir: %w", err)
	}
	defer os.RemoveAll(workdir)

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, b.path, argv...)
	cmd.Dir = workdir
	cmd.Env = builderEnv(workdir)
	// Group the child so Cancel below kills grandchildren too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		slog.Debug("document builder call failed", "argv", len(argv), "stderr", stderr.String())
		if tctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%w after %s: %s", ErrTimeout, timeout, sanitizeStderr(stderr.String()))
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return "", fmt.Errorf("%w (exit %d): %s", ErrExit, exitErr.ExitCode(), sanitizeStderr(stderr.String()))
		}
		return "", fmt.Errorf("%w: %v", ErrExit, runErr)
	}
	return stdout.String(), nil
}

// builderEnv builds the minimal child environment: only the allowlisted
// PATH/LANG/TMPDIR entries plus HOME pointed at the per-call temp dir.
func builderEnv(home string) []string {
	env := []string{"HOME=" + home}
	for _, k := range []string{"PATH", "LANG", "TMPDIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// sanitizeStderr strips absolute paths and truncates to maxStderrSnippet
// chars, so raw stderr is never passed back to the MCP client verbatim.
func sanitizeStderr(s string) string {
	s = absPathSeg.ReplaceAllString(s, "[path]")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxStderrSnippet {
		s = string(r[:maxStderrSnippet-3]) + "..."
	}
	return s
}
