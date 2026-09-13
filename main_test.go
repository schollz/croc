package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

const interruptHelperEnvironment = "CROC_INTERRUPT_HELPER"
const secondInterruptHelperEnvironment = "CROC_SECOND_INTERRUPT_HELPER"

func TestWaitingSendExitsPromptlyOnInterrupt(t *testing.T) {
	if relayAddress := os.Getenv(interruptHelperEnvironment); relayAddress != "" {
		os.Args = []string{
			"croc", "--relay", relayAddress, "--ignore-stdin", "--disable-clipboard",
			"send", "--no-local", "--text", "interrupt-test",
		}
		main()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt cannot be sent to a child process on Windows")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ready := make(chan struct{})
	relayDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			relayDone <- err
			return
		}
		defer connection.Close()
		if err = connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			relayDone <- err
			return
		}
		if _, err = io.ReadFull(connection, make([]byte, 1)); err != nil {
			relayDone <- err
			return
		}
		close(ready)
		_, err = io.Copy(io.Discard, connection)
		relayDone <- err
	}()

	command := exec.Command(os.Args[0], "-test.run=^TestWaitingSendExitsPromptlyOnInterrupt$")
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), interruptHelperEnvironment+"="+listener.Addr().String(), "CROC_CONFIG_DIR="+t.TempDir(), "CROC_SECRET=interrupt-test-code")
	command.Stdout = io.Discard
	var output bytes.Buffer
	command.Stderr = &output
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	var waitErr error
	waitDone := make(chan struct{})
	go func() {
		waitErr = command.Wait()
		close(waitDone)
	}()
	stopCommand := func() {
		_ = command.Process.Kill()
		<-waitDone
	}
	defer stopCommand()

	select {
	case <-ready:
	case <-waitDone:
		t.Fatalf("helper exited before reaching relay: %v; stderr: %s", waitErr, output.String())
	case err = <-relayDone:
		stopCommand()
		t.Fatalf("helper did not start relay handshake: %v; stderr: %s", err, output.String())
	case <-time.After(5 * time.Second):
		stopCommand()
		t.Fatalf("helper send did not become ready; stderr: %s", output.String())
	}
	started := time.Now()
	if err = command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	deadline := 500 * time.Millisecond
	if raceDetectorEnabled {
		deadline = 2 * time.Second
	}
	select {
	case <-waitDone:
		elapsed := time.Since(started)
		if waitErr != nil {
			t.Fatalf("interrupted send exited with %v; stderr: %s", waitErr, output.String())
		}
		if elapsed >= deadline {
			t.Fatalf("interrupted send took %s to exit; stderr: %s", elapsed, output.String())
		}
		if strings.Contains(strings.ToLower(output.String()), "context canceled") {
			t.Fatalf("interrupted send printed a cancellation error: %s", output.String())
		}
	case <-time.After(deadline):
		stopCommand()
		t.Fatalf("interrupted send did not exit within %s; stderr: %s", deadline, output.String())
	}
	select {
	case err = <-relayDone:
		if err != nil {
			t.Fatalf("relay connection did not close cleanly: %v", err)
		}
	case <-time.After(deadline):
		t.Fatal("relay connection remained open after interrupted send")
	}
}

func TestSecondInterruptForcesImmediateExit(t *testing.T) {
	if os.Getenv(secondInterruptHelperEnvironment) == "1" {
		runCLIContext = func(ctx context.Context) error {
			fmt.Fprintln(os.Stderr, "second-interrupt-ready")
			<-ctx.Done()
			fmt.Fprintln(os.Stderr, "first-interrupt-observed")
			select {}
		}
		main()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt cannot be sent to a child process on Windows")
	}

	command := exec.Command(os.Args[0], "-test.run=^TestSecondInterruptForcesImmediateExit$")
	command.Env = append(os.Environ(), secondInterruptHelperEnvironment+"=1")
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()

	ready := make(chan struct{})
	firstObserved := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			switch scanner.Text() {
			case "second-interrupt-ready":
				close(ready)
			case "first-interrupt-observed":
				close(firstObserved)
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("second-interrupt helper did not become ready")
	}
	if err = command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstObserved:
	case <-time.After(2 * time.Second):
		t.Fatal("helper did not observe the first interrupt")
	}
	// The command goroutine observes cancellation just before main restores
	// default signal handling, so allow that scheduling handoff to complete.
	time.Sleep(25 * time.Millisecond)
	started := time.Now()
	if err = command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()
	deadline := 500 * time.Millisecond
	if raceDetectorEnabled {
		deadline = 2 * time.Second
	}
	select {
	case err = <-waitDone:
		if err == nil {
			t.Fatal("second interrupt unexpectedly allowed graceful completion")
		}
		if elapsed := time.Since(started); elapsed >= deadline {
			t.Fatalf("second interrupt took %s to force exit", elapsed)
		}
	case <-time.After(deadline):
		_ = command.Process.Kill()
		t.Fatalf("second interrupt did not force exit within %s", deadline)
	}
}
