//go:build windows

package main

import "os"

func ignoreSIGPIPE() {}

func signalExitCode(os.Signal) int { return 130 }
