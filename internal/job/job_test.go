package job

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunnerSuccess(t *testing.T) {
	r := New()
	if !r.Start("test", func(ctx context.Context, log func(string)) error {
		log("hello")
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
