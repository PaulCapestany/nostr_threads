package main

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestServiceStartup(t *testing.T) {
	cmd := exec.Command("nostr_threads", "b1ae9ebeedc87d416227cf5563307188ec8f7f102e22cf3fa9f81c378cada159")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start the service: %v", err)
	}

	t.Log("Service started successfully.")
	time.Sleep(2 * time.Second)

	t.Log("Sending interrupt signal to the service.")
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatalf("Failed to send interrupt signal: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Service did not shut down gracefully: %v", err)
		}
		t.Log("Service shut down gracefully.")
	case <-time.After(35 * time.Second):
		t.Fatal("Service did not shut down in time.")
	}
}
