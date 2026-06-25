//go:build windows

package scriptcrawler

import "os/exec"

func configureDryRunCommand(cmd *exec.Cmd) {}

func killDryRunOSProcess(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
