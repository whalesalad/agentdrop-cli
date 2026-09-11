//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// ignoreSIGPIPE makes writes to a closed stdout return EPIPE so the CLI can
// exit cleanly instead of dying with a signal status.
func ignoreSIGPIPE() { signal.Ignore(syscall.SIGPIPE) }

func signalExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 130
}
