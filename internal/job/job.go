package job

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type Status int

const (
	StatusIdle Status = iota
	StatusRunning
	StatusOK
	StatusFail
)

func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusOK:
		return "success"
	case StatusFail:
		return "failed"
	default:
		return "idle"
	}
}

type Runner struct {
	mu     sync.Mutex
	lines  []string
	status Status
	err    error
	cancel context.CancelFunc
}

func New() *Runner {
	return &Runner{status: StatusIdle}
}

func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status == StatusRunning
}

func (r *Runner) Snapshot() ([]string, Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]string, len(r.lines))
	copy(cp, r.lines)
	return cp, r.status, r.err
}

func (r *Runner) Cancel() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Runner) Start(title string, fn func(ctx context.Context, log func(string)) error) bool {
	r.mu.Lock()
	if r.status == StatusRunning {
		r.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.status = StatusRunning
	r.err = nil
	r.lines = []string{"— " + title + " —"}
	r.mu.Unlock()

	go func() {
		err := fn(ctx, r.append)
		r.mu.Lock()
		defer r.mu.Unlock()
		r.cancel = nil
		if ctx.Err() != nil {
			r.status = StatusFail
			r.err = context.Canceled
			r.lines = append(r.lines, "CANCELED")
			return
		}
		if err != nil {
			r.status = StatusFail
			r.err = err
			r.lines = append(r.lines, "FAILED: "+err.Error())
			return
		}
		r.status = StatusOK
		r.err = nil
		r.lines = append(r.lines, "SUCCESS")
	}()
	return true
}

func (r *Runner) Append(line string) {
	r.append(line)
}

func (r *Runner) append(line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
}

func Wait(r *Runner, timeout time.Duration) (Status, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, st, err := r.Snapshot()
		if st != StatusRunning {
			return st, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return StatusRunning, errors.New("timeout")
}
