package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

const managementPrefix = "/v0/management/quota-schedule-refresh"
const resourceStatusPath = "/status"

func (r *Runtime) registerManagement() []byte {
	return envelopeResult(map[string]any{
		"routes": []map[string]string{
			{"method": http.MethodGet, "path": managementPrefix + "/status"},
			{"method": http.MethodGet, "path": managementPrefix + "/auth-files"},
			{"method": http.MethodPost, "path": managementPrefix + "/run"},
		},
		"resources": []map[string]string{
			{"path": resourceStatusPath, "menu": "额度定时刷新", "description": "每天按设定时刻刷新 Codex 额度窗口。"},
		},
	}, nil)
}

func (r *Runtime) handleManagement(ctx context.Context, raw []byte) []byte {
	request, err := decodeManagementRequest(raw)
	if err != nil {
		return failure(err)
	}
	httpRequest, err := request.toHTTPRequest(ctx)
	if err != nil {
		return failure(err)
	}
	recorder := httptest.NewRecorder()
	r.serveHTTP(recorder, httpRequest)
	return envelopeResult(map[string]any{
		"StatusCode":   recorder.Code,
		"content_type": recorder.Header().Get("Content-Type"),
		"Headers":      recorder.Header(),
		"Body":         base64.StdEncoding.EncodeToString(recorder.Body.Bytes()),
	}, nil)
}

func (r *Runtime) serveHTTP(w http.ResponseWriter, request *http.Request) {
	path := request.URL.Path
	switch {
	case request.Method == http.MethodGet && (path == resourceStatusPath || path == "/"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(statusPageHTML))
	case request.Method == http.MethodGet && strings.HasSuffix(path, "/auth-files"):
		writeJSON(w, http.StatusOK, map[string]any{"files": r.listCredentials(request.Context())})
	case request.Method == http.MethodGet && strings.HasSuffix(path, "/status"):
		writeJSON(w, http.StatusOK, r.currentStatus())
	case request.Method == http.MethodPost && strings.HasSuffix(path, "/run"):
		var payload struct {
			AuthIDs []string `json:"auth_ids"`
		}
		body, _ := io.ReadAll(request.Body)
		if len(body) > 0 {
			_ = json.Unmarshal(body, &payload)
		}
		r.runOnce(request.Context(), "manual", payload.AuthIDs)
		writeJSON(w, http.StatusOK, r.currentStatus())
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type managementRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Query   url.Values
	Body    []byte
}

func decodeManagementRequest(raw []byte) (managementRequest, error) {
	if len(raw) == 0 {
		return managementRequest{}, fmt.Errorf("%w: management request is required", ErrInvalidRequest)
	}
	var wire struct {
		Method      string      `json:"Method"`
		MethodLower string      `json:"method"`
		Path        string      `json:"Path"`
		PathLower   string      `json:"path"`
		Headers     http.Header `json:"Headers"`
		Query       url.Values  `json:"Query"`
		Body        string      `json:"Body"`
		BodyLower   string      `json:"body"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return managementRequest{}, fmt.Errorf("%w: decode management request", ErrInvalidRequest)
	}
	body := []byte(wire.BodyLower)
	if wire.Body != "" {
		decoded, err := base64.StdEncoding.DecodeString(wire.Body)
		if err != nil {
			return managementRequest{}, fmt.Errorf("%w: decode management body", ErrInvalidRequest)
		}
		body = decoded
	}
	request := managementRequest{
		Method:  firstNonBlank(wire.Method, wire.MethodLower),
		Path:    firstNonBlank(wire.Path, wire.PathLower),
		Headers: wire.Headers,
		Query:   wire.Query,
		Body:    body,
	}
	if request.Method == "" || request.Path == "" {
		return managementRequest{}, fmt.Errorf("%w: management method and path are required", ErrInvalidRequest)
	}
	return request, nil
}

func (r managementRequest) toHTTPRequest(ctx context.Context) (*http.Request, error) {
	path := normalizeManagementPath(r.Path)
	if r.Query != nil && r.Query.Encode() != "" {
		path += "?" + r.Query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, r.Method, path, bytes.NewBuffer(r.Body))
	if err != nil {
		return nil, err
	}
	request.Header = r.Headers
	return request, nil
}

func normalizeManagementPath(path string) string {
	const resourcePrefix = "/v0/resource/plugins/quota-schedule-refresh"
	if path == resourcePrefix {
		return "/"
	}
	if strings.HasPrefix(path, resourcePrefix+"/") {
		return strings.TrimPrefix(path, resourcePrefix)
	}
	return path
}
