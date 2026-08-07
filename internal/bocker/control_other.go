//go:build !linux

package bocker

func shouldUsePrivilegedBroker(args []string) bool { return false }

func runPrivilegedBrokerCommand(args []string) (int, error) { return 0, nil }

func runPrivilegedOperation(args []string) (bool, error) { return false, nil }
