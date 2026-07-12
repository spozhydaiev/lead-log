package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewWithWriterJSONProducesValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(Config{Level: "debug", Format: "json"}, &buf)
	logger.Info("application starting", "llm_base_host", SafeHost("https://api.openai.com/v1?key=secret"))
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got["llm_base_host"] != "api.openai.com" {
		t.Fatalf("unexpected host: %+v", got)
	}
	if strings.Contains(buf.String(), "secret") {
		t.Fatalf("log leaked secret: %s", buf.String())
	}
}

func TestHashPrefix(t *testing.T) {
	if got := HashPrefix("1234567890"); got != "12345678" {
		t.Fatalf("got %q", got)
	}
}
