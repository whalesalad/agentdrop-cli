// Package browser launches the platform browser with a URL argument. It never
// interpolates a shell command.
package browser

import (
	"errors"
	"os/exec"
	"runtime"
	"time"
)

// Open launches the default browser for url. It returns once the opener exits
// or after a short grace period, whichever comes first.
func Open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return errors.New("browser opener is unavailable")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return errors.New("browser opener failed")
		}
		return nil
	case <-time.After(5 * time.Second):
		// Some openers block until the browser exits; assume launch succeeded.
		return nil
	}
}
