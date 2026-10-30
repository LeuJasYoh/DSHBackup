package sidecar

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type recordingWriter struct {
	frames [][]byte
}

func (w *recordingWriter) Write(frame []byte) (int, error) {
	w.frames = append(w.frames, append([]byte{}, frame...))
	return len(frame), nil
}

func (w *recordingWriter) Close() error { return nil }

func TestCallDoesNotWriteAlreadyCancelledRequest(t *testing.T) {
	writer := &recordingWriter{}
	client := &Client{stdin: writer, pending: make(map[int64]chan callResult)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Call(ctx, "install_staging", map[string]any{}, nil); err == nil {
		t.Fatal("cancelled call unexpectedly succeeded")
	}
	if len(writer.frames) != 0 {
		t.Fatalf("cancelled request was written: %d frames", len(writer.frames))
	}
}

func TestCancelStopsRegisteredWorker(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	sidecarPath := filepath.Join(repo, "sidecar", "target", "release", "dsh-backup-core.exe")
	if _, err := os.Stat(sidecarPath); err != nil {
		t.Skip("sidecar 未构建")
	}
	client, err := Start(sidecarPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.CallTask(ctx, "cancel-test", "sleep", map[string]any{"millis": 5000}, nil, nil)
	}()
	time.Sleep(100 * time.Millisecond)
	if err := client.Cancel("cancel-test"); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !IsCancelled(err) {
			t.Fatalf("sleep was not reported as cancelled: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled worker did not finish")
	}
}
