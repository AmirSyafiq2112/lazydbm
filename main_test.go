package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	version = "1.2.3"
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := run([]string{"-version"}, func() (string, error) { return "", errors.New("unused") }, nil, stdout, stderr)
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "1.2.3" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunUnknownFlag(t *testing.T) {
	stderr := &bytes.Buffer{}
	code := run([]string{"-nope"}, func() (string, error) { return ".", nil }, nil, &bytes.Buffer{}, stderr)
	if code != 2 {
		t.Fatalf("code = %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected flag error")
	}
}

func TestRunGetwdError(t *testing.T) {
	stderr := &bytes.Buffer{}
	code := run(nil, func() (string, error) { return "", errors.New("no cwd") }, nil, &bytes.Buffer{}, stderr)
	if code != 1 || !strings.Contains(stderr.String(), "working directory") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunStartsTUI(t *testing.T) {
	var gotCwd, gotVer string
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := run(nil, func() (string, error) { return "/proj", nil }, func(cwd, ver string) error {
		gotCwd, gotVer = cwd, ver
		return nil
	}, stdout, stderr)
	if code != 0 || gotCwd != "/proj" || gotVer != version {
		t.Fatalf("code=%d cwd=%q ver=%q", code, gotCwd, gotVer)
	}
}

func TestRunTUIError(t *testing.T) {
	stderr := &bytes.Buffer{}
	code := run(nil, func() (string, error) { return "/proj", nil }, func(string, string) error {
		return errors.New("boom")
	}, &bytes.Buffer{}, stderr)
	if code != 1 || !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}
