package config

import (
	"bytes"
	"log"
	"os"

	"github.com/joho/godotenv"
)

func LoadEnv() {
	err := godotenv.Load()
	if err == nil {
		return
	}

	// Fallback for editors that save .env with UTF-8 BOM.
	raw, readErr := os.ReadFile(".env")
	if readErr != nil {
		log.Printf("Failed to load .env (%v), using system environment variables", err)
		return
	}

	clean := bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	values, parseErr := godotenv.Unmarshal(string(clean))
	if parseErr != nil {
		log.Printf("Failed to parse .env after BOM fallback (%v), using system environment variables", parseErr)
		return
	}

	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			if setErr := os.Setenv(key, value); setErr != nil {
				log.Printf("Failed to set env key %s: %v", key, setErr)
			}
		}
	}
}

func GetEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
