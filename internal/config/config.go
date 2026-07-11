package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port           int
	MongoURI       string
	MongoDatabase  string
	NATSURL        string
	IngestParserPath string
	WorkspaceDir   string
}

func Load() (*Config, error) {
	port, err := getEnvInt("PORT", 8080)
	if err != nil {
		return nil, fmt.Errorf("PORT: %w", err)
	}
	return &Config{
		Port:            port,
		MongoURI:        getEnv("MONGO_URI", "mongodb://root:dev2@mongodb:27017/dev2knowledge?authSource=admin"),
		MongoDatabase:   getEnv("MONGO_DATABASE", "dev2knowledge"),
		NATSURL:         getEnv("NATS_URL", "nats://localhost:4223"),
		IngestParserPath: getEnv("INGEST_PARSER_PATH", "ingest-parser"),
		WorkspaceDir:    getEnv("WORKSPACE_DIR", "/data/workspace"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return n, nil
}
