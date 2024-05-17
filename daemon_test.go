// daemon_test.go
package main

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestServiceStartup(t *testing.T) {
    cmd := exec.Command("go", "run", "main.go")
    if err := cmd.Start(); err != nil {
        t.Fatalf("Failed to start the service: %v", err)
    }

    // Give it some time to start
    time.Sleep(2 * time.Second)

    if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
        t.Fatalf("Failed to send SIGTERM to the service: %v", err)
    }

    // Wait for the process to exit
    if err := cmd.Wait(); err != nil {
        t.Fatalf("Service did not shut down gracefully: %v", err)
    }
}
