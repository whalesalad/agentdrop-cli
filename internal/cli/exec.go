package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
)

// runChild executes the command with inherited streams and the provided
// environment. Ctrl-C is left to the child; termination signals are forwarded.
func runChild(ctx context.Context, command string, args []string, env []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = env
	if f, ok := stdin.(*os.File); ok {
		cmd.Stdin = f
	} else {
		cmd.Stdin = stdin
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return 1, err
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, forwardedSignals()...)
	defer signal.Stop(signals)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case sig := <-signals:
			if sig == os.Interrupt {
				continue // the terminal already delivered it to the child
			}
			_ = cmd.Process.Signal(sig)
		case <-ctx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
			<-done
			return exitStatus(cmd), nil
		case <-done:
			return exitStatus(cmd), nil
		}
	}
}
