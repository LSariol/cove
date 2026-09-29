package cli

import (
	"bytes"
	"testing"
)

// captureOutput redirects stdout/stderr to buffers and turns color off for the
// duration of the test.
func captureOutput(t *testing.T) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var o, e bytes.Buffer
	oldOut, oldErr, oldColor := stdout, stderr, useColor
	stdout, stderr, useColor = &o, &e, false
	t.Cleanup(func() { stdout, stderr, useColor = oldOut, oldErr, oldColor })
	return &o, &e
}

func TestMessagesGoToStderrWithSymbols(t *testing.T) {
	o, e := captureOutput(t)

	success(`Created "x".`)
	warn("Usage: get <key>")
	fail(`No secret named "x".`)
	out("the-value")

	wantErr := "✓ Created \"x\".\n! Usage: get <key>\n✗ No secret named \"x\".\n"
	if e.String() != wantErr {
		t.Errorf("stderr = %q, want %q", e.String(), wantErr)
	}
	if o.String() != "the-value\n" {
		t.Errorf("stdout = %q, want only the data", o.String())
	}
}

func TestNoColorCodesWhenColorIsOff(t *testing.T) {
	_, e := captureOutput(t)
	fail("boom")
	if bytes.Contains(e.Bytes(), []byte("\033[")) {
		t.Errorf("color codes written with color off: %q", e.String())
	}
}

func TestPromptShowsEnvironment(t *testing.T) {
	captureOutput(t)
	for env, want := range map[string]string{
		"":     "cove> ",
		"DEV":  "cove (dev)> ",
		"prod": "cove (prod)> ",
	} {
		if got := promptFor(env); got != want {
			t.Errorf("promptFor(%q) = %q, want %q", env, got, want)
		}
	}

	useColor = true
	if got := promptFor("PROD"); got != "cove "+red+"(prod)"+reset+"> " {
		t.Errorf("prod prompt isn't red: %q", got)
	}
}
