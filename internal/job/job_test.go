package job

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStatusString(t *testing.T) {
	if StatusIdle.String() != "idle" || StatusRunning.String() != "running" || StatusOK.String() != "success" || StatusFail.String() != "failed" {
		t.Fatal(StatusIdle.String(), StatusRunning.String(), StatusOK.String(), StatusFail.String())
	}
}

func TestRunnerSuccess(t *testing.T) {
	r := New()
	if r.Running() {
		t.Fatal("new runner should not be running")
	}
	if !r.Start("test", func(ctx context.Context, log func(string)) error {
		log("hello")
		log("")
		return nil
	}) {
		t.Fatal("start failed")
	}
	st, err := Wait(r, 2*time.Second)
	if err != nil || st != StatusOK {
		t.Fatalf("status=%s err=%v", st, err)
	}
	lines, _, _ := r.Snapshot()
	if len(lines) < 2 || lines[len(lines)-1] != "SUCCESS" {
		t.Fatalf("lines = %#v", lines)
	}
	copied, _, _ := r.Snapshot()
	copied[0] = "mutated"
	lines, _, _ = r.Snapshot()
	if lines[0] == "mutated" {
		t.Fatal("snapshot should copy")
	}
}

func TestRunnerFail(t *testing.T) {
	r := New()
	r.Start("fail", func(ctx context.Context, log func(string)) error {
		return errors.New("boom")
	})
	st, err := Wait(r, 2*time.Second)
	if st != StatusFail || err == nil {
		t.Fatalf("status=%s err=%v", st, err)
	}
}

func TestRunnerOneAtATime(t *testing.T) {
	r := New()
	started := make(chan struct{})
	block := make(chan struct{})
	r.Start("one", func(ctx context.Context, log func(string)) error {
		close(started)
		<-block
		return nil
	})
	<-started
	if r.Start("two", func(ctx context.Context, log func(string)) error { return nil }) {
		t.Fatal("second job should be rejected")
	}
	close(block)
	_, _ = Wait(r, 2*time.Second)
}

func TestRunnerCancel(t *testing.T) {
	r := New()
	r.Cancel()
	r.Start("cancel", func(ctx context.Context, log func(string)) error {
		<-ctx.Done()
		return ctx.Err()
	})
	r.Cancel()
	st, err := Wait(r, 2*time.Second)
	if st != StatusFail || !errors.Is(err, context.Canceled) {
		t.Fatalf("status=%s err=%v", st, err)
	}
}

func TestWaitTimeout(t *testing.T) {
	r := New()
	block := make(chan struct{})
	r.Start("slow", func(ctx context.Context, log func(string)) error {
		<-block
		return nil
	})
	st, err := Wait(r, 20*time.Millisecond)
	if st != StatusRunning || err == nil {
		t.Fatalf("status=%s err=%v", st, err)
	}
	close(block)
	_, _ = Wait(r, 2*time.Second)
}
