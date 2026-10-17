package config

import "testing"

func TestConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("FAMILY_TIMEZONE", "Europe/Minsk")
	t.Setenv("BOT_ENABLED", "false")
	config, err := Load()
	if err != nil || config.Location.String() != "Europe/Minsk" || config.BotEnabled {
		t.Fatalf("invalid config %+v %v", config, err)
	}
	t.Setenv("FAMILY_TIMEZONE", "invalid/timezone")
	if _, err := Load(); err == nil {
		t.Fatal("invalid timezone accepted")
	}
}
