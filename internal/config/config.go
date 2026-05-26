package config

import (
	"os"
	"time"
)

type Config struct {
	AppPort  string
	Timezone *time.Location
	DBPath   string
}

func LoadConfig() (*Config, error) {
	// Default to UTC+8
	tzStr := os.Getenv("TZ")
	if tzStr == "" {
		tzStr = "Asia/Shanghai"
	}

	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		// Fallback to FixedZone if LoadLocation fails (e.g. on Windows without tzdata)
		loc = time.FixedZone("UTC+8", 8*60*60)
	}

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8389"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "kt-ai-studio.db"
	}

	return &Config{
		AppPort:  port,
		Timezone: loc,
		DBPath:   dbPath,
	}, nil
}
