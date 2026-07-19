package dynacat

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

const editorMaxBodyBytes = 1 << 20 // 1 MiB

func (a *application) handleEditorSchema(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}
	writeJSON(w, http.StatusOK, allWidgetSchemas())
}

// handleEditorStatus returns a generation that changes whenever the config hot-reloads,
// letting the editor wait for the server to pick up a page add/remove before navigating.
func (a *application) handleEditorStatus(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"generation": a.CreatedAt.UnixNano()})
}

func (a *application) handleEditorConfigLoad(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}

	view, err := a.buildEditorConfigView()
	if err != nil {
		slog.Error("Editor config load failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *application) handleEditorConfigSave(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, editorMaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid body")
		return
	}

	var mutation editorMutation
	if err := json.Unmarshal(body, &mutation); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := a.applyEditorMutation(mutation); err != nil {
		switch err.(type) {
		case *editorPermissionError:
			writeJSONError(w, http.StatusForbidden, err.Error())
		case *editorValidationError:
			writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			slog.Error("Editor mutation failed", "op", mutation.Op, "error", err)
			writeJSONError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
