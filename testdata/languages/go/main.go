package main

import (
	"fmt"
	"os"
)

func main() {
	message, err := os.ReadFile("/etc/go-mount-message")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("/var/lib/go-mount-app/result.txt", []byte("go-mounted\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("{\"language\":\"go\",\"message\":%q}\n", string(message))
}
