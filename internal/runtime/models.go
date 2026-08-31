package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"quota-schedule-refresh/internal/host"
)

func (r *Runtime) cpaModels() []string {
	if r == nil || r.host == nil {
		return nil
	}
	port, apiKey := localCPAListen()
	if apiKey == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/v1/models"
	response, err := r.host.HTTPDo(ctx, host.HTTPRequest{
		Method: http.MethodGet,
		URL:    url,
		Headers: host.Header{
			"Authorization": {"Bearer " + apiKey},
			"Accept":        {"application/json"},
		},
	})
	if err != nil || !host.IsHTTPSuccess(response.StatusCode) {
		response, err = localModelsDirect(url, apiKey)
		if err != nil || !host.IsHTTPSuccess(response.StatusCode) {
			return nil
		}
	}
	return parseOpenAIModelIDs(response.Body)
}

func localModelsDirect(url, apiKey string) (host.HTTPResponse, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return host.HTTPResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	client := &http.Client{
		Timeout:   8 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	resp, err := client.Do(request)
	if err != nil {
		return host.HTTPResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return host.HTTPResponse{}, err
	}
	return host.HTTPResponse{StatusCode: resp.StatusCode, Body: body}, nil
}

func parseOpenAIModelIDs(raw []byte) []string {
	var document struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &document) != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, item := range document.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || seen[id] {
			continue
		}
		lower := strings.ToLower(id)
		owned := strings.ToLower(item.OwnedBy)
		if strings.Contains(lower, "image") || strings.Contains(lower, "video") || strings.Contains(lower, "imagine") {
			continue
		}
		openai := owned == "openai" || strings.Contains(lower, "gpt") || strings.Contains(lower, "codex")
		if !openai {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func localCPAListen() (int, string) {
	port := 8317
	apiKey := ""
	for _, path := range []string{"/CLIProxyAPI/config.yaml", "config.yaml"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		inKeys := false
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(trimmed, "port:") {
				if parsed, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "port:"))); err == nil && parsed > 0 {
					port = parsed
				}
				continue
			}
			if trimmed == "api-keys:" {
				inKeys = true
				continue
			}
			if inKeys {
				if strings.HasPrefix(trimmed, "-") {
					value := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
					value = strings.Trim(value, `"'`)
					if value != "" && apiKey == "" {
						apiKey = value
					}
					continue
				}
				if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
					inKeys = false
				}
			}
		}
		if apiKey != "" {
			return port, apiKey
		}
	}
	return port, apiKey
}
