package wake

import (
	"strings"
	"testing"
)

func TestSummarizeModelReplyCodexCompleted(t *testing.T) {
	body := `{&#34;type&#34;:&#34;response.completed&#34;,&#34;response&#34;:{&#34;id&#34;:&#34;resp_abc&#34;,&#34;object&#34;:&#34;response&#34;,&#34;status&#34;:&#34;completed&#34;,&#34;output&#34;:[{&#34;type&#34;:&#34;message&#34;,&#34;content&#34;:[{&#34;type&#34;:&#34;output_text&#34;,&#34;text&#34;:&#34;Hello from Codex.&#34;}]}]}}`
	got := summarizeModelReply([]byte(body))
	if got != "Hello from Codex." {
		t.Fatalf("got %q", got)
	}
}

func TestSummarizeModelReplySSE(t *testing.T) {
	body := "event: response.output_text.delta\ndata: {\"delta\":\"Hel\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"content\":[{\"text\":\"Hello there.\"}]}]}}\n"
	got := summarizeModelReply([]byte(body))
	if got != "Hello there." {
		t.Fatalf("got %q", got)
	}
}

func TestSummarizeModelReplyChatCompletions(t *testing.T) {
	body := `{"choices":[{"message":{"content":"Quota window refreshed."}}]}`
	got := summarizeModelReply([]byte(body))
	if got != "Quota window refreshed." {
		t.Fatalf("got %q", got)
	}
}

func TestSummarizeModelReplyFallsBackToStatus(t *testing.T) {
	body := `{"type":"response.completed","response":{"id":"resp_abc","status":"completed","background":true}}`
	got := summarizeModelReply([]byte(body))
	if got != "completed" {
		t.Fatalf("got %q", got)
	}
}

func TestSummarizeModelReplyErrorMessage(t *testing.T) {
	body := `{"error":{"message":"rate limited"}}`
	got := summarizeModelReply([]byte(body))
	if got != "rate limited" {
		t.Fatalf("got %q", got)
	}
}

func TestSummarizeModelReplyDoesNotDumpJSON(t *testing.T) {
	body := `{&#34;type&#34;:&#34;response.completed&#34;,&#34;response&#34;:{&#34;id&#34;:&#34;resp_abc&#34;,&#34;status&#34;:&#34;completed&#34;}}`
	got := summarizeModelReply([]byte(body))
	if strings.Contains(got, "{") || strings.Contains(got, "&#34;") || strings.Contains(got, "resp_") {
		t.Fatalf("dumped raw payload: %q", got)
	}
}
