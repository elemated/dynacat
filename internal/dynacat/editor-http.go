package dynacat

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

const (
	editorMaxBodyBytes      = 1 << 20
	editorNotAllowedMessage = "you are not allowed to use the web UI editor"
)

func (a *application) handleEditorSchema(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}
	writeJSON(w, http.StatusOK, allWidgetSchemas())
}

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
	user := a.getAuthenticatedUser(w, r)
	if !a.userCanEditAnything(user) {
		writeJSONError(w, http.StatusForbidden, editorNotAllowedMessage)
		return
	}

	view, err := a.buildEditorConfigView(user)
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
	user := a.getAuthenticatedUser(w, r)
	if !a.userCanEditAnything(user) {
		writeJSONError(w, http.StatusForbidden, editorNotAllowedMessage)
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

	if err := a.applyEditorMutation(user, mutation); err != nil {
		switch err.(type) {
		case *editorPermissionError, *editorDisabledError:
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

type editorConvertRequest struct {
	To    string          `json:"to"`
	Value json.RawMessage `json:"value"`
	Text  string          `json:"text"`
}

// Converts a list field between its structured form and its YAML text so the
// editor can offer both views without a YAML library in the browser.
func (a *application) handleEditorConvert(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}
	if !a.userCanEditAnything(a.getAuthenticatedUser(w, r)) {
		writeJSONError(w, http.StatusForbidden, editorNotAllowedMessage)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, editorMaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid body")
		return
	}

	var req editorConvertRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	switch req.To {
	case "yaml":
		var value any = []any{}
		if len(req.Value) > 0 {
			if err := json.Unmarshal(req.Value, &value); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid value")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"text": nodeToText(valueNode(value))})
	case "value":
		node, err := parseYAMLValue(req.Text)
		if err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		var value any
		if err := node.Decode(&value); err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "unsupported YAML structure")
			return
		}
		writeJSON(w, http.StatusOK, map[string]json.RawMessage{"value": encoded})
	default:
		writeJSONError(w, http.StatusBadRequest, "unknown conversion")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
