//go:build windows

package instagram

import "os/exec"

func startInOwnProcessGroup(cmd *exec.Cmd) {}

// killProcessTree kills only the direct child on Windows; cmd.WaitDelay keeps
// Wait from hanging on pipes a surviving grandchild still holds.
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
