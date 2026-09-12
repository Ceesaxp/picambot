package motion

import (
	"context"
	"fmt"
	"os/exec"
)

// runSystemctl executes "sudo systemctl <action> motion".
func runSystemctl(ctx context.Context, action string) error {
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", action, "motion")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s motion: %w: %s", action, err, out)
	}
	return nil
}
