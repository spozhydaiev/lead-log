package config

import (
	"testing"
	"time"
)

func TestAllowedTelegramUserIDsRequired(t *testing.T) {
	for _, raw := range []string{"", "   ", ","} {
		t.Run(raw, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("mustAllowedUsers did not panic for empty allowlist")
				}
			}()
			_ = mustAllowedUsers(raw)
		})
	}
}

func TestIsTelegramUserAllowedRequiresExplicitAllowlistEntry(t *testing.T) {
	if IsTelegramUserAllowed(nil, 100) {
		t.Fatal("nil allowlist should not allow arbitrary users")
	}
	if IsTelegramUserAllowed(map[int64]bool{}, 100) {
		t.Fatal("empty allowlist should not allow arbitrary users")
	}
	allowed := map[int64]bool{100: true}
	if !IsTelegramUserAllowed(allowed, 100) {
		t.Fatal("explicit user should be allowed")
	}
	if IsTelegramUserAllowed(allowed, 200) {
		t.Fatal("missing user should not be allowed")
	}
}

func TestSummaryTimeoutMustExceedLLMTimeout(t *testing.T) {
	llm := 75 * time.Second
	summary := 90 * time.Second
	if summary <= llm+10*time.Second {
		t.Fatal("summary timeout must leave validation and persistence headroom beyond LLM timeout")
	}
}
