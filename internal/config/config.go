package config

import "os"

// Config — всё, что приложение берёт из окружения (.env локально, environment в compose).
type Config struct {
	Port        string
	DatabaseURL string
}

func Load() Config {
	return Config{
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/app?sslmode=disable"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
