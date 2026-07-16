package logging

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRuntimeLoggingDoesNotUseSensitiveAttributeNames(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	forbidden := []string{
		"telegram_user_id", "telegram_chat_id", "telegram_message_id", "telegram_update_id",
		"user_id", "note_id", "person_id", "person_name", "alias", "ticket_key",
		"entity_value", "question", "raw_text", "note_text", "summary", "snippet",
		"action_title", "decision_text", "people_note_text", "chat_id",
	}
	logCall := regexp.MustCompile(`\.(Info|Warn|Error|Debug)\s*\(`)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "privacy_scan_test.go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if !logCall.MatchString(line) {
				continue
			}
			for _, field := range forbidden {
				if strings.Contains(line, `"`+field+`"`) {
					t.Fatalf("sensitive logging attribute %q in %s:%d", field, path, i+1)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOperationIDsAreRandomAndOpaque(t *testing.T) {
	first := NewOperationID()
	second := NewOperationID()
	if first == "" || second == "" || first == second {
		t.Fatalf("operation ids must be non-empty and unique: %q %q", first, second)
	}
	for _, sensitive := range []string{"191155356", "CH-1234"} {
		if strings.Contains(first, sensitive) || strings.Contains(second, sensitive) {
			t.Fatalf("operation id contains sensitive fixture %q", sensitive)
		}
	}
}
