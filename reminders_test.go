package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
)

// TestStatusForCode verifies the script-code → HTTP-status mapping.
func TestStatusForCode(t *testing.T) {
	cases := map[string]int{
		"not_found":   http.StatusNotFound,
		"bad_request": http.StatusBadRequest,
		"unsupported": http.StatusNotImplemented,
		"error":       http.StatusInternalServerError,
		"":            http.StatusInternalServerError,
	}
	for code, want := range cases {
		if got := statusForCode(code); got != want {
			t.Errorf("statusForCode(%q) = %d, want %d", code, got, want)
		}
	}
}

// TestRunReminderSuccess feeds runReminder a trivial JXA script that returns a
// success envelope. It exercises the JSON-arg passing and envelope decoding
// without touching the Reminders database, so it is safe to run in CI.
func TestRunReminderSuccess(t *testing.T) {
	script := `function run(argv) {
		var input = JSON.parse(argv[0] || '{}');
		return JSON.stringify({ ok: true, data: { echo: input.value } });
	}`
	data, opErr := runReminder(script, map[string]interface{}{"value": "hello \"world\" \\ and 'quotes'"})
	if opErr != nil {
		t.Fatalf("expected success, got error: %+v", opErr)
	}
	var out struct {
		Echo string `json:"echo"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("failed to decode data: %v", err)
	}
	// Confirms user input round-trips through the JSON argv without any
	// escaping/injection mangling.
	if out.Echo != `hello "world" \ and 'quotes'` {
		t.Errorf("input not round-tripped, got %q", out.Echo)
	}
}

// TestRunReminderError feeds runReminder an error envelope and verifies the
// code is mapped to the right HTTP status and message. Safe for CI.
func TestRunReminderError(t *testing.T) {
	script := `function run(argv) {
		return JSON.stringify({ ok: false, code: "not_found", error: "nope" });
	}`
	_, opErr := runReminder(script, nil)
	if opErr == nil {
		t.Fatal("expected an error, got nil")
	}
	if opErr.Status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", opErr.Status, http.StatusNotFound)
	}
	if opErr.Message != "nope" {
		t.Errorf("message = %q, want %q", opErr.Message, "nope")
	}
}

// TestRemindersLifecycle runs the full create→read→complete→edit→delete
// lifecycle against the real Reminders app. It is OPT-IN (set
// MOWA_REMINDERS_LIVE_TEST=1) because it requires macOS Automation permission
// and would otherwise hang CI on a TCC prompt. Per issue #11's hard constraint,
// it operates ONLY on a throwaway list it creates, and deletes only that list.
func TestRemindersLifecycle(t *testing.T) {
	if os.Getenv("MOWA_REMINDERS_LIVE_TEST") != "1" {
		t.Skip("set MOWA_REMINDERS_LIVE_TEST=1 to run the live Reminders lifecycle test")
	}

	listName := fmt.Sprintf("mowa-test-%d", os.Getpid())
	t.Logf("using throwaway list %q", listName)

	// Create list
	data, opErr := runReminder(scriptCreateList, map[string]interface{}{"name": listName})
	if opErr != nil {
		t.Fatalf("create list: %+v", opErr)
	}
	var list ReminderList
	mustDecode(t, data, &list)
	if list.Name != listName || list.ID == "" {
		t.Fatalf("unexpected created list: %+v", list)
	}
	// Guarantee cleanup of ONLY the list we created, even on failure.
	defer func() {
		if _, err := runReminder(scriptDeleteList, map[string]interface{}{"id": list.ID}); err != nil {
			t.Errorf("cleanup delete list: %+v", err)
		}
	}()

	// Create reminder with notes + due date
	due := "2026-08-01T09:00:00Z"
	data, opErr = runReminder(scriptCreateReminder, map[string]interface{}{
		"list":     list.ID,
		"name":     "buy milk",
		"notes":    "2 liters",
		"due_date": due,
	})
	if opErr != nil {
		t.Fatalf("create reminder: %+v", opErr)
	}
	var rem Reminder
	mustDecode(t, data, &rem)
	if rem.ID == "" || rem.Name != "buy milk" || rem.Notes != "2 liters" {
		t.Fatalf("unexpected created reminder: %+v", rem)
	}
	if rem.DueDate == nil {
		t.Fatalf("expected a due date, got nil")
	}

	// List: incomplete-only should include it
	assertListCount(t, list.ID, false, 1)

	// Mark complete
	data, opErr = runReminder(scriptUpdateReminder, map[string]interface{}{
		"id":        rem.ID,
		"completed": true,
	})
	if opErr != nil {
		t.Fatalf("complete reminder: %+v", opErr)
	}
	mustDecode(t, data, &rem)
	if !rem.Completed {
		t.Fatalf("reminder not marked complete: %+v", rem)
	}

	// Default listing excludes completed; completed=true includes it
	assertListCount(t, list.ID, false, 0)
	assertListCount(t, list.ID, true, 1)

	// Edit name, notes and due date
	data, opErr = runReminder(scriptUpdateReminder, map[string]interface{}{
		"id":       rem.ID,
		"name":     "buy oat milk",
		"notes":    "barista edition",
		"due_date": "2026-08-02T10:00:00Z",
	})
	if opErr != nil {
		t.Fatalf("edit reminder: %+v", opErr)
	}
	mustDecode(t, data, &rem)
	if rem.Name != "buy oat milk" || rem.Notes != "barista edition" {
		t.Fatalf("edit did not apply: %+v", rem)
	}

	// A missing id returns not_found
	if _, err := runReminder(scriptUpdateReminder, map[string]interface{}{
		"id":   "x-apple-reminder://does-not-exist",
		"name": "x",
	}); err == nil || err.Status != http.StatusNotFound {
		t.Fatalf("expected 404 for missing reminder, got %+v", err)
	}

	// Delete the reminder
	if _, opErr = runReminder(scriptDeleteReminder, map[string]interface{}{"id": rem.ID}); opErr != nil {
		t.Fatalf("delete reminder: %+v", opErr)
	}
	assertListCount(t, list.ID, true, 0)
}

func assertListCount(t *testing.T, listID string, completed bool, want int) {
	t.Helper()
	data, opErr := runReminder(scriptListReminders, map[string]interface{}{
		"id":        listID,
		"completed": completed,
	})
	if opErr != nil {
		t.Fatalf("list reminders (completed=%v): %+v", completed, opErr)
	}
	var rems []Reminder
	mustDecode(t, data, &rems)
	if len(rems) != want {
		t.Fatalf("list reminders (completed=%v): got %d, want %d", completed, len(rems), want)
	}
}

func mustDecode(t *testing.T, data json.RawMessage, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode: %v (raw: %s)", err, string(data))
	}
}
