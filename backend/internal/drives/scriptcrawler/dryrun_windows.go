//go:build windows

package scriptcrawler

import (
	"os/exec"
	"strconv"
)

func configureDryRunCommand(cmd *exec.Cmd) {}

func killDryRunOSProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	taskkill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	if err := taskkill.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
