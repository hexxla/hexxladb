package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"log/slog"
)

// Config holds process configuration loaded from the environment.
type Config struct {
	ListenAddr          string
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	ShutdownTimeout     time.Duration
	LogLevel            slog.Level
	MaxRequestBodyBytes int64
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	readTO, err := durationEnv("HTTP_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	writeTO, err := durationEnv("HTTP_WRITE_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	idleTO, err := durationEnv("HTTP_IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return Config{}, err
	}
	shutdownTO, err := durationEnv("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	levelStr := os.Getenv("LOG_LEVEL")
	if levelStr == "" {
		levelStr = "INFO"
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.TrimSpace(levelStr))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	maxBody := int64(1 << 20) // 1 MiB, aligned with [domain.MaxContentLen]
	if v := os.Getenv("HTTP_MAX_BODY_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("HTTP_MAX_BODY_BYTES: %w", err)
		}
		if n < 1 {
			return Config{}, fmt.Errorf("HTTP_MAX_BODY_BYTES: must be >= 1")
		}
		maxBody = n
	}

	if readTO <= 0 || writeTO <= 0 || idleTO <= 0 || shutdownTO <= 0 {
		return Config{}, fmt.Errorf("timeouts must be positive")
	}

	return Config{
		ListenAddr:          addr,
		ReadTimeout:         readTO,
		WriteTimeout:        writeTO,
		IdleTimeout:         idleTO,
		ShutdownTimeout:     shutdownTO,
		LogLevel:            level,
		MaxRequestBodyBytes: maxBody,
	}, nil
}

func durationEnv(key string, defaultD time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return defaultD, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
	}
	return d, nil
}
