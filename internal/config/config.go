package config

import (
	"github.com/joho/godotenv"
)

// Load reads an optional .env file into the process environment.
// Stream knobs (HOST_IP, HLS_TIME, HLS_LIST_SIZE, AI_OFFSET_MS) are read
// via os.Getenv where they are used. No secrets are required to run.
func Load() {
	_ = godotenv.Load()
}
