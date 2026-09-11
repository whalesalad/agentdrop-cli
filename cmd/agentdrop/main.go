// Command agentdrop is the standalone AgentDrop CLI.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/whalesalad/agentdrop-cli/internal/browser"
	"github.com/whalesalad/agentdrop-cli/internal/cli"
)

func main() {
	ignoreSIGPIPE()
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	var received os.Signal
	go func() {
		received = <-signals
		cancel()
	}()
	code := cli.Run(ctx, &cli.Env{
		Args:             os.Args[1:],
		Stdin:            os.Stdin,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		StdinIsTerminal:  isTerminal(os.Stdin),
		StdoutIsTerminal: isTerminal(os.Stdout),
		OpenBrowser:      browser.Open,
	})
	signal.Stop(signals)
	if code == 130 && received != nil {
		code = signalExitCode(received)
	}
	os.Exit(code)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
