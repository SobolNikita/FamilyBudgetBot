package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL   string
	HTTPAddr      string
	WebDir        string
	Currency      string
	Location      *time.Location
	BotEnabled    bool
	TelegramToken string
	LLMBaseURL    string
	LLMKey        string
	LLMModel      string
}

func Load() (Config, error) {
	config := Config{DatabaseURL: os.Getenv("DATABASE_URL"), HTTPAddr: value("HTTP_ADDR", ":8080"), WebDir: value("WEB_DIR", "web/dist"), Currency: value("FAMILY_CURRENCY", "BYN"), TelegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"), LLMBaseURL: value("LLM_BASE_URL", "https://api.openai.com/v1"), LLMKey: os.Getenv("LLM_API_KEY"), LLMModel: os.Getenv("LLM_MODEL")}
	if config.DatabaseURL == "" {
		return config, errors.New("DATABASE_URL is required")
	}
	location, err := time.LoadLocation(value("FAMILY_TIMEZONE", "Europe/Minsk"))
	if err != nil {
		return config, errors.New("invalid FAMILY_TIMEZONE")
	}
	config.Location = location
	if !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(config.Currency) {
		return config, errors.New("FAMILY_CURRENCY must be an uppercase ISO currency code")
	}
	config.BotEnabled, err = strconv.ParseBool(value("BOT_ENABLED", "false"))
	if err != nil {
		return config, errors.New("invalid BOT_ENABLED")
	}
	if config.BotEnabled {
		if config.TelegramToken == "" || config.LLMModel == "" {
			return config, errors.New("TELEGRAM_BOT_TOKEN and LLM_MODEL are required when BOT_ENABLED=true")
		}
		endpoint, err := url.Parse(config.LLMBaseURL)
		if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return config, fmt.Errorf("invalid LLM_BASE_URL")
		}
	}
	return config, nil
}

func value(name, fallback string) string {
	if result := os.Getenv(name); result != "" {
		return result
	}
	return fallback
}
