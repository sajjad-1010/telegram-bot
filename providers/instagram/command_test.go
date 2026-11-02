//go:build !windows

package instagram

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunCommandKeepsStderrOutOfStdout(t *testing.T) {
	cmd := exec.Command("sh", "-c", `echo '{"ok":1}'; echo 'WARNING: noise' >&2`)
	out, err := runCommandWithTimeout(cmd, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != `{"ok":1}` {
		t.Fatalf("stdout = %q, want only the JSON line", got)
	}
}

func TestRunCommandIncludesStderrInError(t *testing.T) {
	cmd := exec.Command("sh", "-c", `echo 'boom' >&2; exit 3`)
	_, err := runCommandWithTimeout(cmd, 10)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want it to carry stderr", err)
	}
}

func TestRunCommandTimeoutKillsGrandchildren(t *testing.T) {
	// The shell's child keeps stdout open; without killing the whole group,
	// Wait would block until the grandchild exits on its own.
	cmd := exec.Command("sh", "-c", "sleep 30 & sleep 30")
	start := time.Now()
	_, err := runCommandWithTimeout(cmd, 1)
	if err == nil || err.Error() != "command timed out" {
		t.Fatalf("err = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("took %s; the process tree was not killed", elapsed)
	}
}
