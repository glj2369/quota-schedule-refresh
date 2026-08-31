package runtime

import (
	"encoding/json"
	"errors"
	"fmt"

	"quota-schedule-refresh/internal/config"
)

var (
	ErrInvalidRequest = errors.New("runtime: invalid request")
	ErrShutdown       = errors.New("runtime: shutdown")
)

type Envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *EnvelopeError  `json:"error,omitempty"`
}

type EnvelopeError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func decodeConfigYAML(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var asBytes struct {
		ConfigYAML []byte `json:"config_yaml"`
	}
	if err := json.Unmarshal(raw, &asBytes); err == nil && len(asBytes.ConfigYAML) > 0 {
		return string(asBytes.ConfigYAML), nil
	}
	var asString struct {
		ConfigYAML string `json:"config_yaml"`
	}
	if err := json.Unmarshal(raw, &asString); err != nil {
		return "", fmt.Errorf("%w: decode json", ErrInvalidRequest)
	}
	return asString.ConfigYAML, nil
}

func envelopeResult(result any, err error) []byte {
	if err != nil {
		return failure(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return failure(err)
	}
	return mustMarshal(Envelope{OK: true, Result: encoded})
}

func envelopeStatus(err error) []byte {
	if err != nil {
		return failure(err)
	}
	return mustMarshal(Envelope{OK: true, Result: json.RawMessage(`{"status":"ok"}`)})
}

func failure(err error) []byte {
	code := "internal_error"
	if errors.Is(err, ErrInvalidRequest) {
		code = "invalid_request"
	}
	if errors.Is(err, config.ErrInvalidConfig) {
		code = "invalid_config"
	}
	if errors.Is(err, ErrShutdown) {
		code = "shutdown"
	}
	return mustMarshal(Envelope{OK: false, Error: &EnvelopeError{Code: code, Message: err.Error()}})
}

func mustMarshal(envelope Envelope) []byte {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return []byte(`{"ok":false,"error":{"code":"internal_error","message":"encode envelope failed"}}`)
	}
	return encoded
}
