package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr           string
	DatabaseURL    string
	SessionKey     []byte
	Environment    string
	IdleTimeout    time.Duration
	AbsoluteMaxAge time.Duration
}

func Load() (Config, error) {
	c := Config{
		Addr:           getenv("ADDR", ":8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		Environment:    getenv("ENV", "dev"),
		IdleTimeout:    14 * 24 * time.Hour,
		AbsoluteMaxAge: 30 * 24 * time.Hour,
	}

	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}

	key := os.Getenv("SESSION_KEY")
	if key == "" {
		if c.Environment != "dev" {
			return c, errors.New("SESSION_KEY is required outside dev")
		}
		key = "dev-only-do-not-use-in-production-please-rotate"
	}
	c.SessionKey = []byte(key)

	if v := os.Getenv("SESSION_IDLE_HOURS"); v != "" {
		hours, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("invalid SESSION_IDLE_HOURS: %w", err)
		}
		c.IdleTimeout = time.Duration(hours) * time.Hour
	}

	return c, nil
}

func (c Config) IsProduction() bool {
	return c.Environment == "prod" || c.Environment == "production"
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
