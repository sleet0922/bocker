//go:build !linux

package bocker

const (
	callerUIDEnv      = "BOCKER_CALLER_UID"
	callerGIDEnv      = "BOCKER_CALLER_GID"
	terminalWidthEnv  = "BOCKER_TERM_WIDTH"
	terminalHeightEnv = "BOCKER_TERM_HEIGHT"
)

func shouldUsePrivilegedBroker(args []string) bool { return false }

func runPrivilegedBrokerCommand(args []string) (int, error) { return 0, nil }
