package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Worker   WorkerConfig
	Provider ProviderConfig
}

type ServerConfig struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type DatabaseConfig struct {
	DSN             string
	MaxConnections  int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type WorkerConfig struct {
	Concurrency      int
	RateLimitPerSec  int // per channel
	PollInterval     time.Duration
	MaxRetries       int
	SchedulerEnabled bool
}

type ProviderConfig struct {
	WebhookURL string
	Timeout    time.Duration
}

// Load reads configuration from environment variables with sane defaults
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Addr:         getEnv("SERVER_ADDR", ":8080"),
			ReadTimeout:  getDuration("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout: getDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
		},
		Database: DatabaseConfig{
			DSN:             getEnv("DATABASE_DSN", "postgres://postgres:postgres@localhost:5432/notifications?sslmode=disable"),
			MaxConnections:  getInt("DB_MAX_CONNECTIONS", 25),
			MaxIdleConns:    getInt("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getInt("REDIS_DB", 0),
		},
		Worker: WorkerConfig{
			Concurrency:      getInt("WORKER_CONCURRENCY", 10),
			RateLimitPerSec:  getInt("WORKER_RATE_LIMIT", 100),
			PollInterval:     getDuration("WORKER_POLL_INTERVAL", 500*time.Millisecond),
			MaxRetries:       getInt("WORKER_MAX_RETRIES", 5),
			SchedulerEnabled: getBool("SCHEDULER_ENABLED", true),
		},
		Provider: ProviderConfig{
			WebhookURL: getEnv("PROVIDER_WEBHOOK_URL", "https://webhook.site/your-uuid-here"),
			Timeout:    getDuration("PROVIDER_TIMEOUT", 10*time.Second),
		},
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
