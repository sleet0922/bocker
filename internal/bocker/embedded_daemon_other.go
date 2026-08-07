//go:build !linux

package bocker

import (
	"fmt"
)

func ensureEmbeddedDaemon() error {
	return fmt.Errorf("the embedded Bocker container runtime is available only on Linux")
}

func runEmbeddedDaemonSupervisor() error {
	return fmt.Errorf("the embedded Bocker container runtime is available only on Linux")
}
