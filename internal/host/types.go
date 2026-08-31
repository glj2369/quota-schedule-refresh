package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type Client interface {
	ModelExecute(ctx context.Context, request ModelExecuteRequest) (ModelExecuteResponse, error)
	HTTPDo(ctx context.Context, request HTTPRequest) (HTTPResponse, error)
	ListAuthFiles(ctx context.Context) ([]AuthFile, error)
	GetAuthFile(ctx context.Context, authIndex string) (AuthFile, error)
	GetRuntimeAuthFile(ctx context.Context, authIndex string) (AuthFile, error)
	SaveAuthFile(ctx context.Context, name string, data []byte) error
}

type Header map[string][]string

type HTTPRequest struct {
	Method  string `json:"Method"`
	URL     string `json:"URL"`
	Headers Header `json:"Headers,omitempty"`
	Body    []byte `json:"Body,omitempty"`
}

type HTTPResponse struct {
	StatusCode int
	Headers    Header
	Body       []byte
}

func (r *HTTPResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		StatusCode      *int    `json:"StatusCode"`
		StatusCodeSnake *int    `json:"status_code"`
		Headers         Header  `json:"Headers"`
		HeadersLower    Header  `json:"headers"`
		Body            *string `json:"Body"`
		BodyLower       *string `json:"body"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.StatusCode != nil {
		r.StatusCode = *raw.StatusCode
	} else if raw.StatusCodeSnake != nil {
		r.StatusCode = *raw.StatusCodeSnake
	}
	if raw.Headers != nil {
		r.Headers = raw.Headers
	} else {
		r.Headers = raw.HeadersLower
	}
	body, err := decodeBody(raw.Body, raw.BodyLower)
	if err != nil {
		return err
	}
	r.Body = body
	return nil
}

type ModelExecuteRequest struct {
	Model   string              `json:"model"`
	Stream  bool                `json:"stream"`
	Body    []byte              `json:"body"`
	Headers map[string][]string `json:"headers"`
}

type ModelExecuteResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

func (r *ModelExecuteResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		StatusCode      *int                `json:"StatusCode"`
		StatusCodeSnake *int                `json:"status_code"`
		Headers         map[string][]string `json:"Headers"`
		HeadersLower    map[string][]string `json:"headers"`
		Body            *string             `json:"Body"`
		BodyLower       *string             `json:"body"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.StatusCode != nil {
		r.StatusCode = *raw.StatusCode
	} else if raw.StatusCodeSnake != nil {
		r.StatusCode = *raw.StatusCodeSnake
	}
	if raw.Headers != nil {
		r.Headers = raw.Headers
	} else {
		r.Headers = raw.HeadersLower
	}
	body, err := decodeBody(raw.Body, raw.BodyLower)
	if err != nil {
		return err
	}
	r.Body = body
	return nil
}

func decodeBody(official *string, legacy *string) ([]byte, error) {
	if official != nil {
		if *official == "" {
			return nil, nil
		}
		decoded, err := base64.StdEncoding.DecodeString(*official)
		if err != nil {
			return nil, fmt.Errorf("decode Body base64: %w", err)
		}
		return decoded, nil
	}
	if legacy == nil || *legacy == "" {
		return nil, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(*legacy); err == nil {
		return decoded, nil
	}
	return []byte(*legacy), nil
}

type AuthFile struct {
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name"`
	AuthIndex    string          `json:"auth_index"`
	Account      string          `json:"account,omitempty"`
	Email        string          `json:"email,omitempty"`
	Provider     string          `json:"provider,omitempty"`
	Type         string          `json:"type,omitempty"`
	Disabled     bool            `json:"disabled"`
	RecentModels []string        `json:"recent_models,omitempty"`
	Models       []string        `json:"models,omitempty"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
	Attributes   map[string]any  `json:"attributes,omitempty"`
	ModelQuotas  json.RawMessage `json:"model_quotas,omitempty"`
	Data         []byte          `json:"data,omitempty"`
}

func IsHTTPSuccess(statusCode int) bool {
	return statusCode >= 200 && statusCode <= 299
}
