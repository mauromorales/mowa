package main

import (
	"strings"
	"testing"
	"time"
)

// TestExecuteAppleScriptSuccess verifies a trivial script returns quickly with no error.
func TestExecuteAppleScriptSuccess(t *testing.T) {
	start := time.Now()
	if err := executeAppleScript(`return "ok"`, 15*time.Second); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("trivial send took %s, expected well under a second", elapsed)
	}
}

// TestExecuteAppleScriptTimeout simulates a hung Messages bridge with `delay` and
// confirms the call fails fast with a timeout error instead of blocking.
func TestExecuteAppleScriptTimeout(t *testing.T) {
	start := time.Now()
	err := executeAppleScript(`delay 30`, 1*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected a timeout error, got: %v", err)
	}
	// context deadline is timeout + 2s grace = ~3s; allow slack for CI.
	if elapsed > 8*time.Second {
		t.Errorf("timeout took %s, expected it to fail fast (~3s)", elapsed)
	}
}
