package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestRunToolCancelsAnUncooperativeProcessTree(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunTool(ctx, executable, dir, "-test.run=^TestCodecProcessHelper$", "--", "parent", dir)
	}()
	pidPath := filepath.Join(dir, "child.pid")
	deadline := time.Now().Add(15 * time.Second)
	var pid uint32
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(pidPath)
		if readErr == nil {
			value, parseErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			pid = uint32(value)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("codec did not start its child")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid) //nolint:misspell // SYNCHRONIZE is the Windows API constant's spelling.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("codec cancellation did not finish")
	}
	result, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("codec child survived cancellation: result=%d err=%v", result, err)
	}
}

func TestCodecProcessHelper(t *testing.T) {
	if len(os.Args) < 5 || os.Args[len(os.Args)-3] != "--" {
		return
	}
	mode, dir := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if mode == "parent" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.CommandContext(t.Context(), executable, "-test.run=^TestCodecProcessHelper$", "--", "child", dir)
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "child.pid"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = child.Wait() }()
	}
	// An external codec does not cooperate with Go context cancellation.
	time.Sleep(30 * time.Second)
}
