//go:build linux

package browser

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// InitVirtualDisplay starts Xvfb (and x11vnc when available) when DISPLAY is unset.
// Production Linux servers have no physical screen; Connect browser needs :99 like docker-entrypoint.sh.
func InitVirtualDisplay() error {
	if os.Getenv("DISPLAY") != "" {
		return nil
	}
	if _, err := exec.LookPath("Xvfb"); err != nil {
		return fmt.Errorf("DISPLAY is unset and Xvfb is not installed — use the Jobifai Docker image or install xvfb, x11vnc, and novnc")
	}
	cmd := exec.Command("Xvfb", ":99", "-screen", "0", "1280x800x24", "-nolisten", "tcp")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Xvfb: %w", err)
	}
	time.Sleep(time.Second)
	if err := os.Setenv("DISPLAY", ":99"); err != nil {
		return fmt.Errorf("set DISPLAY: %w", err)
	}
	if vnc, err := exec.LookPath("x11vnc"); err == nil {
		vncCmd := exec.Command(vnc, "-display", ":99", "-nopw", "-listen", "localhost", "-xkb", "-forever", "-shared", "-quiet")
		vncCmd.Stdout = os.Stderr
		vncCmd.Stderr = os.Stderr
		_ = vncCmd.Start()
	}
	return nil
}
