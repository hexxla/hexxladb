package config

import (
	"fmt"
	"os"
	"strings"

	"log/slog"
)

// Config holds process configuration loaded from the environment.
type Config struct {
	LogLevel slog.Level
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	levelStr := os.Getenv("LOG_LEVEL")
	if levelStr == "" {
		levelStr = "INFO"
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.TrimSpace(levelStr))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	return Config{LogLevel: level}, nil
}
