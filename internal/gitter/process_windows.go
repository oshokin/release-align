package gitter

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// configureProcess kills the Git process tree when the command is canceled.
func (c *Client) configureProcess(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		killer := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		if err := killer.Run(); err != nil {
			return cmd.Process.Kill()
		}

		return nil
	}
}
