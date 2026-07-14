package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// defaultReminderTimeoutSeconds bounds a single Reminders osascript call. It is
// higher than the messages timeout because Reminders scripting can be slow on
// large databases, but still well under osascript's ~120s default AppleEvent
// timeout so a wedged Reminders process fails fast instead of hanging the
// request.
const defaultReminderTimeoutSeconds = 30

// The Reminders endpoints drive Reminders.app through JavaScript for Automation
// (JXA) via `osascript -l JavaScript`. Two properties make this robust:
//
//   - All caller-supplied input is passed as a single JSON string argument
//     (argv[0]) and parsed with JSON.parse inside the script, then applied
//     through the scripting API. User data is never concatenated into the
//     script text, so there is no AppleScript-injection or quote-escaping
//     hazard (contrast messages.go, which must escape quotes).
//   - Each script emits a single JSON envelope via JSON.stringify, so Go parses
//     structured output instead of splitting delimiter-separated text that
//     could collide with names/notes containing separators or newlines.
//
// The first Reminders call triggers a macOS Automation (TCC) permission prompt;
// this is documented in the README and the endpoint descriptions below.

// jxaEnvelope is the uniform result every Reminders script returns. Scripts trap
// their own errors and report them here (with a code) rather than exiting
// non-zero, so a not-found or unsupported operation is distinguishable from a
// genuine crash.
type jxaEnvelope struct {
	OK    bool            `json:"ok"`
	Code  string          `json:"code"`
	Error string          `json:"error"`
	Data  json.RawMessage `json:"data"`
}

// reminderOpError carries an HTTP status alongside a client-facing message so
// handlers can translate a failed script run into the right response.
type reminderOpError struct {
	Status  int
	Message string
}

// reminderTimeout returns the configured Reminders osascript timeout, falling
// back to the default when no config is loaded or the value is invalid.
func reminderTimeout() time.Duration {
	if appConfig != nil && appConfig.Reminders.TimeoutSeconds > 0 {
		return time.Duration(appConfig.Reminders.TimeoutSeconds) * time.Second
	}
	return defaultReminderTimeoutSeconds * time.Second
}

// statusForCode maps a script-reported error code to an HTTP status.
func statusForCode(code string) int {
	switch code {
	case "not_found":
		return http.StatusNotFound
	case "bad_request":
		return http.StatusBadRequest
	case "unsupported":
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

// runReminder marshals input to JSON, runs the JXA script with it as argv[0]
// under a bounded timeout, and decodes the JSON envelope. On success it returns
// the raw data payload for the caller to unmarshal; otherwise it returns a
// reminderOpError with the appropriate HTTP status.
func runReminder(script string, input interface{}) (json.RawMessage, *reminderOpError) {
	argJSON := "{}"
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return nil, &reminderOpError{http.StatusInternalServerError, "failed to encode request"}
		}
		argJSON = string(b)
	}

	timeout := reminderTimeout()
	output, timedOut, err := runOSAScript(timeout, "-l", "JavaScript", "-e", script, argJSON)
	if timedOut {
		log.Printf("Reminders script timed out after %s; killed osascript", timeout)
		return nil, &reminderOpError{http.StatusInternalServerError, err.Error()}
	}
	if err != nil {
		// A non-zero exit here means the script threw before it could emit an
		// envelope (e.g. TCC permission denied). Surface the raw output.
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		log.Printf("Reminders script failed: %v; output: %s", err, msg)
		return nil, &reminderOpError{http.StatusInternalServerError, msg}
	}

	var env jxaEnvelope
	if e := json.Unmarshal(bytes.TrimSpace(output), &env); e != nil {
		log.Printf("Reminders script produced unparseable output: %s", string(output))
		return nil, &reminderOpError{http.StatusInternalServerError, "failed to parse Reminders output"}
	}
	if !env.OK {
		return nil, &reminderOpError{statusForCode(env.Code), env.Error}
	}
	return env.Data, nil
}

// reminderError writes a reminderOpError as a JSON error response.
func reminderError(c echo.Context, opErr *reminderOpError) error {
	return c.JSON(opErr.Status, ReminderErrorResponse{Error: opErr.Message})
}

// pathID returns the list/reminder id from a URL path parameter. Ids contain
// characters such as "://" (e.g. "x-apple-reminder://<uuid>") that a client
// must percent-encode to keep them within a single path segment. Echo matches
// on the escaped path and returns the still-encoded segment, so we unescape it
// here before handing it to the JXA scripts; otherwise "%2F" would never match
// the literal "/" in the real id and every lookup would 404. A value with no
// escapes (or an invalid escape) is returned unchanged.
func pathID(raw string) string {
	if decoded, err := url.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

// @Summary List Reminders lists
// @Description Return every list in the macOS Reminders app with its name and stable id. The first call triggers a macOS Automation (TCC) permission prompt for controlling Reminders. macOS only.
// @Tags reminders
// @Produce json
// @Success 200 {object} ReminderListsResponse "Lists retrieved successfully"
// @Failure 500 {object} ReminderErrorResponse "Internal server error"
// @Router /api/reminders/lists [get]
func handleListReminderLists(c echo.Context) error {
	data, opErr := runReminder(scriptListLists, nil)
	if opErr != nil {
		return reminderError(c, opErr)
	}
	var lists []ReminderList
	if err := json.Unmarshal(data, &lists); err != nil {
		return c.JSON(http.StatusInternalServerError, ReminderErrorResponse{Error: "failed to decode lists"})
	}
	return c.JSON(http.StatusOK, ReminderListsResponse{Lists: lists})
}

// @Summary Create a Reminders list
// @Description Create a new list in the macOS Reminders app. macOS only.
// @Tags reminders
// @Accept json
// @Produce json
// @Param request body CreateListRequest true "List to create"
// @Success 201 {object} ReminderList "List created"
// @Failure 400 {object} ReminderErrorResponse "Bad request - invalid input"
// @Failure 500 {object} ReminderErrorResponse "Internal server error"
// @Router /api/reminders/lists [post]
func handleCreateReminderList(c echo.Context) error {
	var req CreateListRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "invalid request body"})
	}
	if strings.TrimSpace(req.Name) == "" {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "name is required"})
	}
	data, opErr := runReminder(scriptCreateList, map[string]interface{}{"name": req.Name})
	if opErr != nil {
		return reminderError(c, opErr)
	}
	var list ReminderList
	if err := json.Unmarshal(data, &list); err != nil {
		return c.JSON(http.StatusInternalServerError, ReminderErrorResponse{Error: "failed to decode list"})
	}
	return c.JSON(http.StatusCreated, list)
}

// @Summary Delete a Reminders list
// @Description Delete a list and all reminders it contains, addressed by its stable id (percent-encode the id in the path). macOS only.
// @Tags reminders
// @Produce json
// @Param id path string true "List id (percent-encoded)"
// @Success 204 "List deleted"
// @Failure 404 {object} ReminderErrorResponse "List not found"
// @Failure 500 {object} ReminderErrorResponse "Internal server error"
// @Router /api/reminders/lists/{id} [delete]
func handleDeleteReminderList(c echo.Context) error {
	id := pathID(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "list id is required"})
	}
	if _, opErr := runReminder(scriptDeleteList, map[string]interface{}{"id": id}); opErr != nil {
		return reminderError(c, opErr)
	}
	return c.NoContent(http.StatusNoContent)
}

// @Summary List reminders in a list
// @Description Return the reminders in a list, addressed by its stable id (percent-encode the id in the path). Completed reminders are excluded unless completed=true. macOS only.
// @Tags reminders
// @Produce json
// @Param id path string true "List id (percent-encoded)"
// @Param completed query bool false "Include completed reminders (default false)"
// @Success 200 {object} RemindersResponse "Reminders retrieved successfully"
// @Failure 404 {object} ReminderErrorResponse "List not found"
// @Failure 500 {object} ReminderErrorResponse "Internal server error"
// @Router /api/reminders/lists/{id}/reminders [get]
func handleListReminders(c echo.Context) error {
	id := pathID(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "list id is required"})
	}
	includeCompleted := c.QueryParam("completed") == "true"
	data, opErr := runReminder(scriptListReminders, map[string]interface{}{
		"id":        id,
		"completed": includeCompleted,
	})
	if opErr != nil {
		return reminderError(c, opErr)
	}
	var reminders []Reminder
	if err := json.Unmarshal(data, &reminders); err != nil {
		return c.JSON(http.StatusInternalServerError, ReminderErrorResponse{Error: "failed to decode reminders"})
	}
	return c.JSON(http.StatusOK, RemindersResponse{Reminders: reminders})
}

// @Summary Create a reminder
// @Description Create a reminder in a list (addressed by id or name). notes and due_date are optional; due_date is RFC3339. macOS only.
// @Tags reminders
// @Accept json
// @Produce json
// @Param request body CreateReminderRequest true "Reminder to create"
// @Success 201 {object} Reminder "Reminder created"
// @Failure 400 {object} ReminderErrorResponse "Bad request - invalid input"
// @Failure 404 {object} ReminderErrorResponse "List not found"
// @Failure 500 {object} ReminderErrorResponse "Internal server error"
// @Router /api/reminders [post]
func handleCreateReminder(c echo.Context) error {
	var req CreateReminderRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "invalid request body"})
	}
	if strings.TrimSpace(req.List) == "" {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "list is required"})
	}
	if strings.TrimSpace(req.Name) == "" {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "name is required"})
	}

	input := map[string]interface{}{
		"list": req.List,
		"name": req.Name,
	}
	if req.Notes != "" {
		input["notes"] = req.Notes
	}
	if req.DueDate != "" {
		t, err := time.Parse(time.RFC3339, req.DueDate)
		if err != nil {
			return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "due_date must be an RFC3339 timestamp"})
		}
		input["due_date"] = t.Format(time.RFC3339)
	}

	data, opErr := runReminder(scriptCreateReminder, input)
	if opErr != nil {
		return reminderError(c, opErr)
	}
	var reminder Reminder
	if err := json.Unmarshal(data, &reminder); err != nil {
		return c.JSON(http.StatusInternalServerError, ReminderErrorResponse{Error: "failed to decode reminder"})
	}
	return c.JSON(http.StatusCreated, reminder)
}

// @Summary Update a reminder
// @Description Update any of name, notes, due_date (RFC3339) or completed on a reminder addressed by its stable id (percent-encode the id in the path). Moving a reminder to another list (the list field) is not supported by the macOS Reminders scripting interface and returns 501. macOS only.
// @Tags reminders
// @Accept json
// @Produce json
// @Param id path string true "Reminder id (percent-encoded)"
// @Param request body UpdateReminderRequest true "Fields to update"
// @Success 200 {object} Reminder "Reminder updated"
// @Failure 400 {object} ReminderErrorResponse "Bad request - invalid input"
// @Failure 404 {object} ReminderErrorResponse "Reminder not found"
// @Failure 501 {object} ReminderErrorResponse "Operation not supported (moving between lists)"
// @Failure 500 {object} ReminderErrorResponse "Internal server error"
// @Router /api/reminders/{id} [patch]
func handleUpdateReminder(c echo.Context) error {
	id := pathID(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "reminder id is required"})
	}

	var req UpdateReminderRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "invalid request body"})
	}

	// Moving a reminder between lists is not supported by the Reminders
	// scripting interface. Report it honestly rather than faking it.
	if req.List != nil {
		return c.JSON(http.StatusNotImplemented, ReminderErrorResponse{
			Error: "moving a reminder between lists is not supported by the macOS Reminders scripting interface",
		})
	}

	input := map[string]interface{}{"id": id}
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "name cannot be empty"})
		}
		input["name"] = *req.Name
	}
	if req.Notes != nil {
		input["notes"] = *req.Notes
	}
	if req.Completed != nil {
		input["completed"] = *req.Completed
	}
	if req.DueDate != nil {
		t, err := time.Parse(time.RFC3339, *req.DueDate)
		if err != nil {
			return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "due_date must be an RFC3339 timestamp"})
		}
		input["due_date"] = t.Format(time.RFC3339)
	}

	if len(input) == 1 { // only the id
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "no updatable fields provided"})
	}

	data, opErr := runReminder(scriptUpdateReminder, input)
	if opErr != nil {
		return reminderError(c, opErr)
	}
	var reminder Reminder
	if err := json.Unmarshal(data, &reminder); err != nil {
		return c.JSON(http.StatusInternalServerError, ReminderErrorResponse{Error: "failed to decode reminder"})
	}
	return c.JSON(http.StatusOK, reminder)
}

// @Summary Delete a reminder
// @Description Delete a reminder addressed by its stable id (percent-encode the id in the path). macOS only.
// @Tags reminders
// @Produce json
// @Param id path string true "Reminder id (percent-encoded)"
// @Success 204 "Reminder deleted"
// @Failure 404 {object} ReminderErrorResponse "Reminder not found"
// @Failure 500 {object} ReminderErrorResponse "Internal server error"
// @Router /api/reminders/{id} [delete]
func handleDeleteReminder(c echo.Context) error {
	id := pathID(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, ReminderErrorResponse{Error: "reminder id is required"})
	}
	if _, opErr := runReminder(scriptDeleteReminder, map[string]interface{}{"id": id}); opErr != nil {
		return reminderError(c, opErr)
	}
	return c.NoContent(http.StatusNoContent)
}

// --- JXA scripts -------------------------------------------------------------
//
// Each script defines run(argv); osascript prints run's return value. argv[0] is
// the JSON input. Every script returns a JSON envelope: {ok, data} on success or
// {ok:false, code, error} on failure. Errors are trapped so the process exits 0
// and Go can map codes to HTTP statuses.

// remObjFn is a shared JXA helper (inlined into scripts that return a single
// reminder) that serialises a reminder specifier to the API shape.
//
// It reads every scalar field in a single r.properties() round-trip rather than
// one AppleEvent per property. This matters: on this app, per-property reads of
// a reminder can take ~30s (enough to trip the request timeout), while a single
// properties() call plus one container.name() lookup runs in ~10s.
const remObjFn = `
function remObj(r) {
  var p = r.properties();
  var listName = "";
  try { listName = p.container.name(); } catch (e) {}
  return {
    id: p.id,
    name: p.name,
    notes: (p.body === null || p.body === undefined) ? "" : p.body,
    due_date: p.dueDate ? p.dueDate.toISOString() : null,
    completed: p.completed,
    completion_date: p.completionDate ? p.completionDate.toISOString() : null,
    list: listName
  };
}
`

const scriptListLists = `
function run(argv) {
  try {
    var R = Application('Reminders');
    var lists = R.lists;
    var names = lists.name();
    var ids = lists.id();
    var out = [];
    for (var i = 0; i < names.length; i++) {
      out.push({ name: names[i], id: ids[i] });
    }
    return JSON.stringify({ ok: true, data: out });
  } catch (e) {
    return JSON.stringify({ ok: false, code: "error", error: String(e) });
  }
}
`

const scriptCreateList = `
function run(argv) {
  try {
    var input = JSON.parse(argv[0] || '{}');
    var R = Application('Reminders');
    var l = R.List({ name: input.name });
    R.lists.push(l);
    return JSON.stringify({ ok: true, data: { name: l.name(), id: l.id() } });
  } catch (e) {
    return JSON.stringify({ ok: false, code: "error", error: String(e) });
  }
}
`

const scriptDeleteList = `
function run(argv) {
  try {
    var input = JSON.parse(argv[0] || '{}');
    var R = Application('Reminders');
    var lists = R.lists;
    var ids = lists.id();
    var idx = ids.indexOf(input.id);
    if (idx < 0) {
      return JSON.stringify({ ok: false, code: "not_found", error: "list not found: " + input.id });
    }
    R.delete(lists[idx]);
    return JSON.stringify({ ok: true, data: {} });
  } catch (e) {
    return JSON.stringify({ ok: false, code: "error", error: "failed to delete list: " + String(e) });
  }
}
`

const scriptListReminders = `
function run(argv) {
  try {
    var input = JSON.parse(argv[0] || '{}');
    var includeCompleted = input.completed === true;
    var R = Application('Reminders');
    var lists = R.lists;
    var ids = lists.id();
    var idx = ids.indexOf(input.id);
    if (idx < 0) {
      return JSON.stringify({ ok: false, code: "not_found", error: "list not found: " + input.id });
    }
    var l = lists[idx];
    var listName = l.name();
    var rem = l.reminders;
    var rids = rem.id();
    var names = rem.name();
    var bodies = rem.body();
    var comp = rem.completed();
    var due = rem.dueDate();
    var cdate = rem.completionDate();
    var out = [];
    for (var i = 0; i < rids.length; i++) {
      if (!includeCompleted && comp[i]) continue;
      out.push({
        id: rids[i],
        name: names[i],
        notes: (bodies[i] === null || bodies[i] === undefined) ? "" : bodies[i],
        due_date: due[i] ? due[i].toISOString() : null,
        completed: comp[i],
        completion_date: cdate[i] ? cdate[i].toISOString() : null,
        list: listName
      });
    }
    return JSON.stringify({ ok: true, data: out });
  } catch (e) {
    return JSON.stringify({ ok: false, code: "error", error: String(e) });
  }
}
`

var scriptCreateReminder = remObjFn + `
function run(argv) {
  try {
    var input = JSON.parse(argv[0] || '{}');
    var R = Application('Reminders');
    var lists = R.lists;
    var ids = lists.id();
    var nms = lists.name();
    var idx = ids.indexOf(input.list);
    if (idx < 0) idx = nms.indexOf(input.list);
    if (idx < 0) {
      return JSON.stringify({ ok: false, code: "not_found", error: "list not found: " + input.list });
    }
    var l = lists[idx];
    var props = { name: input.name };
    if (input.notes !== undefined && input.notes !== null) props.body = input.notes;
    if (input.due_date) props.dueDate = new Date(input.due_date);
    var r = R.Reminder(props);
    l.reminders.push(r);
    return JSON.stringify({ ok: true, data: remObj(r) });
  } catch (e) {
    return JSON.stringify({ ok: false, code: "error", error: String(e) });
  }
}
`

var scriptUpdateReminder = remObjFn + `
function run(argv) {
  try {
    var input = JSON.parse(argv[0] || '{}');
    var R = Application('Reminders');
    var r = R.reminders.byId(input.id);
    try { r.name(); } catch (e) {
      return JSON.stringify({ ok: false, code: "not_found", error: "reminder not found: " + input.id });
    }
    if (input.name !== undefined) r.name = input.name;
    if (input.notes !== undefined) r.body = input.notes;
    if (input.completed !== undefined) r.completed = input.completed;
    if (input.due_date !== undefined) r.dueDate = new Date(input.due_date);
    return JSON.stringify({ ok: true, data: remObj(r) });
  } catch (e) {
    return JSON.stringify({ ok: false, code: "error", error: String(e) });
  }
}
`

const scriptDeleteReminder = `
function run(argv) {
  try {
    var input = JSON.parse(argv[0] || '{}');
    var R = Application('Reminders');
    var r = R.reminders.byId(input.id);
    try { r.name(); } catch (e) {
      return JSON.stringify({ ok: false, code: "not_found", error: "reminder not found: " + input.id });
    }
    R.delete(r);
    return JSON.stringify({ ok: true, data: {} });
  } catch (e) {
    return JSON.stringify({ ok: false, code: "error", error: String(e) });
  }
}
`
