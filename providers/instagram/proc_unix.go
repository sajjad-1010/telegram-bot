//go:build !windows

package instagram

import (
	"os/exec"
	"syscall"
)

// startInOwnProcessGroup puts the child in a new process group so a timeout
// can kill it together with whatever it spawned (yt-dlp -> ffmpeg, py -> ...).
func startInOwnProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// A negative pid signals the whole group started by startInOwnProcessGroup.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
