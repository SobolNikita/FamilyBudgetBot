package config

import (
	"testing"
	"time"
)

func TestConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("FAMILY_TIMEZONE", "")
	t.Setenv("FAMILY_CURRENCY", "")
	t.Setenv("BOT_ENABLED", "false")
	config, err := Load()
	if err != nil || config.Location.String() != "Europe/Moscow" || config.Currency != "RUB" || config.BotEnabled {
		t.Fatalf("invalid config %+v %v", config, err)
	}
	t.Setenv("FAMILY_TIMEZONE", "invalid/timezone")
	if _, err := Load(); err == nil {
		t.Fatal("invalid timezone accepted")
	}
}

func TestLLMTimeoutConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("BOT_ENABLED", "false")
	t.Setenv("LLM_TIMEOUT", "")
	settings, err := Load()
	if err != nil || settings.LLMTimeout != 20*time.Second {
		t.Fatalf("unexpected default timeout: %v %v", settings.LLMTimeout, err)
	}
	for _, value := range []string{"0s", "-1s", "500ms", "3m", "invalid"} {
		t.Setenv("LLM_TIMEOUT", value)
		if _, err := Load(); err == nil {
			t.Fatalf("invalid timeout accepted: %s", value)
		}
	}
	t.Setenv("LLM_TIMEOUT", "30s")
	settings, err = Load()
	if err != nil || settings.LLMTimeout != 30*time.Second {
		t.Fatal("custom timeout ignored")
	}
}
