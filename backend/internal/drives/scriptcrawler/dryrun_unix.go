//go:build !windows

package scriptcrawler

import (
	"os/exec"
	"syscall"
)

func configureDryRunCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killDryRunOSProcess(cmd *exec.Cmd) error {
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		return cmd.Process.Kill()
	}
	return nil
}
