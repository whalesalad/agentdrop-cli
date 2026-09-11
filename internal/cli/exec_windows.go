//go:build windows

package cli

import (
	"os"
	"os/exec"
)

func forwardedSignals() []os.Signal { return []os.Signal{os.Interrupt} }

func exitStatus(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return 1
	}
	if code := cmd.ProcessState.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
