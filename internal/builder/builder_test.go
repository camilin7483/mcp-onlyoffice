package builder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// writeFake creates an executable shell script in dir and returns its path.
func writeFake(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// tempRoots returns a fresh dir plus a roots slice containing its subdir.
func tempRoots(t *testing.T) (base, root string) {
	t.Helper()
	base = t.TempDir()
	root = filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return base, root
}

func TestDetectNonexistentPathUnavailable(t *testing.T) {
	b := Detect(filepath.Join(t.TempDir(), "no-such-docbuilder"))
	if b == nil {
		t.Fatal("Detect must never return nil")
	}
	if b.Available() {
		t.Error("Available = true for a nonexistent explicit path")
	}
	if b.Path() != "" {
		t.Errorf("Path = %q, want empty", b.Path())
	}
	if err := b.Convert(context.Background(), "a", "b"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Convert err = %v, want ErrUnavailable", err)
	}
	if _, err := b.Version(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Version err = %v, want ErrUnavailable", err)
	}
}

func TestDetectEmptyPathWithoutBinaryUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // hermetic: no binaries here
	b := Detect("")
	if b.Available() {
		t.Errorf("Available = true with an empty PATH dir (found %q)", b.Path())
	}
}

func TestDetectPATHLookupAndVersion(t *testing.T) {
	dir := t.TempDir()
	writeFake(t, dir, "docbuilder", "#!/bin/sh\necho 'docbuilder-test 1.2.3'\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	b := Detect("")
	if !b.Available() {
		t.Fatal("Available = false, want true via PATH lookup")
	}
	v, err := b.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if !strings.Contains(v, "1.2.3") {
		t.Errorf("Version = %q, want it to contain 1.2.3", v)
	}
}

func TestDetectExplicitPathPreferred(t *testing.T) {
	dir := t.TempDir()
	explicit := writeFake(t, dir, "custom", "#!/bin/sh\necho explicit 9.9\n")
	writeFake(t, dir, "docbuilder", "#!/bin/sh\necho path 1.0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	b := Detect(explicit)
	if !b.Available() || b.Path() != explicit {
		t.Fatalf("Path = %q, want explicit %q", b.Path(), explicit)
	}
}

func TestConvertSuccessMinimalEnvAndCleanup(t *testing.T) {
	_, root := tempRoots(t)
	bin := t.TempDir()
	homeWitness := filepath.Join(t.TempDir(), "home.txt")
	envWitness := filepath.Join(t.TempDir(), "env.txt")
	writeFake(t, bin, "docbuilder", fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s' \"$HOME\" > %q\nenv > %q\necho converted > \"$2\"\n",
		homeWitness, envWitness))

	in := filepath.Join(root, "in.txt")
	out := filepath.Join(root, "sub", "out.txt")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MCP_BUILDER_TEST_SECRET", "s3cr3t-must-not-leak")
	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	if err := b.Convert(context.Background(), in, out); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if data, err := os.ReadFile(out); err != nil || string(data) != "converted\n" {
		t.Fatalf("output = %q, err = %v", data, err)
	}

	// HOME pointed at a per-call temp dir, not the real home, and it is gone.
	homeRaw, err := os.ReadFile(homeWitness)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(homeRaw); got == "" || got == os.Getenv("HOME") {
		t.Errorf("child HOME = %q, want a temp dir unlike the real HOME", got)
	}
	if _, err := os.Stat(string(homeRaw)); !os.IsNotExist(err) {
		t.Errorf("per-call workdir %q still exists, want it removed", string(homeRaw))
	}
	// No inherited secret in the child environment.
	envRaw, err := os.ReadFile(envWitness)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envRaw), "MCP_BUILDER_TEST_SECRET") {
		t.Error("child environment inherited MCP_BUILDER_TEST_SECRET")
	}
	// No leftover workdirs in the shared temp dir.
	left, err := filepath.Glob(filepath.Join(os.TempDir(), "mcp-onlyoffice-builder-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("leftover builder workdirs: %v", left)
	}
}

func TestConvertTimeoutKillsProcess(t *testing.T) {
	_, root := tempRoots(t)
	bin := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "pid.txt")
	writeFake(t, bin, "docbuilder", fmt.Sprintf(
		"#!/bin/sh\necho $$ > %q\nsleep 30\n", pidFile))
	in := filepath.Join(root, "in.txt")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	b.SetTimeout(500 * time.Millisecond)
	start := time.Now()
	err := b.Convert(context.Background(), in, filepath.Join(root, "out.txt"))
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Convert err = %v, want ErrTimeout", err)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("Convert took %v, timeout kill looks ineffective", time.Since(start))
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("pid witness missing (process never started?): %v", err)
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(raw)), "%d", &pid); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("pid %d still alive after timeout, want the group killed", pid)
	}
}

func TestConvertNonZeroExit(t *testing.T) {
	_, root := tempRoots(t)
	bin := t.TempDir()
	secretAbs := filepath.Join(t.TempDir(), "secret-dir", "boom.txt")
	writeFake(t, bin, "docbuilder", fmt.Sprintf(
		"#!/bin/sh\necho 'failed at %s details, very long stderr padding %s' >&2\nexit 3\n",
		secretAbs, strings.Repeat("x", 300)))
	in := filepath.Join(root, "in.txt")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	err := b.Convert(context.Background(), in, filepath.Join(root, "out.txt"))
	if !errors.Is(err, ErrExit) {
		t.Fatalf("Convert err = %v, want ErrExit", err)
	}
	if strings.Contains(err.Error(), secretAbs) {
		t.Errorf("error leaks an absolute path: %q", err)
	}
	if !strings.Contains(err.Error(), "[path]") {
		t.Errorf("error should scrub paths with [path]: %q", err)
	}
	if len([]rune(err.Error())) > 300 {
		t.Errorf("error too long (%d chars), stderr must be truncated", len([]rune(err.Error())))
	}
}

func TestConvertOutputMissing(t *testing.T) {
	_, root := tempRoots(t)
	bin := t.TempDir()
	writeFake(t, bin, "docbuilder", "#!/bin/sh\nexit 0\n")
	in := filepath.Join(root, "in.txt")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	if err := b.Convert(context.Background(), in, filepath.Join(root, "out.txt")); !errors.Is(err, ErrOutputMissing) {
		t.Errorf("Convert err = %v, want ErrOutputMissing", err)
	}
}

func TestConvertRejectsOutsideRootsWithoutSpawning(t *testing.T) {
	base, root := tempRoots(t)
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "spawned.txt")
	writeFake(t, bin, "docbuilder", fmt.Sprintf("#!/bin/sh\ntouch %q\necho ok > \"$2\"\n", marker))

	outside := filepath.Join(base, "evil.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	out := filepath.Join(root, "out.txt")
	for name, in := range map[string]string{
		"outside roots":  outside,
		"dotdot escape":  filepath.Join(root, "..", "evil.txt"),
		"symlink escape": link,
	} {
		t.Run(name, func(t *testing.T) {
			_ = os.Remove(marker)
			_ = os.Remove(out)
			if err := b.Convert(context.Background(), in, out); !errors.Is(err, ErrOutsideRoots) {
				t.Errorf("Convert err = %v, want ErrOutsideRoots", err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Error("backend spawned despite rejected input (marker exists)")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Error("output was created despite rejected input")
			}
		})
	}
}

func TestConvertRejectsNonRegularInput(t *testing.T) {
	_, root := tempRoots(t)
	bin := t.TempDir()
	writeFake(t, bin, "docbuilder", "#!/bin/sh\necho ok > \"$2\"\n")
	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	if err := b.Convert(context.Background(), root, filepath.Join(root, "out.txt")); err == nil {
		t.Error("expected an error for a directory input")
	} else if errors.Is(err, ErrUnavailable) {
		t.Errorf("unavailable binary must not shadow validation: %v", err)
	}
}

func TestConvertRejectsOversizedInput(t *testing.T) {
	_, root := tempRoots(t)
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "spawned.txt")
	writeFake(t, bin, "docbuilder", fmt.Sprintf("#!/bin/sh\ntouch %q\necho ok > \"$2\"\n", marker))
	// Sparse file just over the 512 MiB cap: instant to create.
	big := filepath.Join(root, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxInputBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()

	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	if err := b.Convert(context.Background(), big, filepath.Join(root, "out.txt")); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Convert err = %v, want ErrTooLarge", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("backend spawned despite oversized input (marker exists)")
	}
}

func TestConvertRejectsOversizedOutput(t *testing.T) {
	_, root := tempRoots(t)
	bin := t.TempDir()
	// Sparse file just over the 1 GiB cap.
	writeFake(t, bin, "docbuilder", "#!/bin/sh\ndd if=/dev/zero of=\"$2\" bs=1 seek=1073741824 count=1 status=none\n")
	in := filepath.Join(root, "in.txt")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	out := filepath.Join(root, "out.bin")
	if err := b.Convert(context.Background(), in, out); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Convert err = %v, want ErrTooLarge", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("oversized output was left behind, want it removed")
	}
}

func TestConvertRejectsURLInput(t *testing.T) {
	_, root := tempRoots(t)
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "spawned.txt")
	writeFake(t, bin, "docbuilder", fmt.Sprintf("#!/bin/sh\ntouch %q\necho ok > \"$2\"\n", marker))

	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	for _, in := range []string{"https://example.com/x.docbuilder", "http://example.com/x"} {
		if err := b.Convert(context.Background(), in, filepath.Join(root, "out.txt")); err == nil {
			t.Errorf("input %q: expected an error", in)
		} else if !errors.Is(err, ErrOutsideRoots) {
			t.Errorf("input %q: err = %v, want ErrOutsideRoots", in, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("backend spawned for URL input (marker exists)")
	}
}

func TestRenderPassesFormat(t *testing.T) {
	_, root := tempRoots(t)
	bin := t.TempDir()
	writeFake(t, bin, "docbuilder", "#!/bin/sh\necho \"got:$2\" > \"$3\"\n")
	in := filepath.Join(root, "in.txt")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := Detect(filepath.Join(bin, "docbuilder"))
	b.SetRoots([]string{root})
	out := filepath.Join(root, "out.pdf")
	if err := b.Render(context.Background(), in, "pdf", out); err != nil {
		t.Fatalf("Render: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "got:pdf") {
		t.Errorf("output = %q, want the format passed through", data)
	}
	if err := b.Render(context.Background(), in, "", out); err == nil {
		t.Error("expected an error for an empty format")
	}
}
