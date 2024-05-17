package main

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestServiceStartup(t *testing.T) {
	// Provide a dummy messageID as an argument
	cmd := exec.Command("nostr_threads", "b1ae9ebeedc87d416227cf5563307188ec8f7f102e22cf3fa9f81c378cada159")

	if err := cmd.Start(); err != nil {
		t.Fatalf("Service failed to start: %v", err)
	}
	t.Log("Service started successfully.")

	// Wait for a short duration to ensure the service has started
	time.Sleep(2 * time.Second)

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("Failed to send interrupt signal to the service: %v", err)
	}
	t.Log("Sent interrupt signal to the service.")

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Service did not shut down gracefully: %v", err)
		} else {
			t.Log("Service shut down gracefully.")
		}
	case <-time.After(5 * time.Second):
		t.Errorf("Service shutdown timed out")
	}
}
