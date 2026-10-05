//go:build !linux && !darwin && !windows

package gitter

import "os/exec"

// configureProcess leaves process-tree cancellation to the host.
func (c *Client) configureProcess(cmd *exec.Cmd) {
}
