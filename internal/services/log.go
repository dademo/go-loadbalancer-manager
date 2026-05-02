package services

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// NewLogger builds the application logger with source-location formatting.
func NewLogger() zerolog.Logger {
	zerolog.CallerMarshalFunc = func(_ uintptr, file string, _ int) string {
		const moduleRoot = "go-loadbalancer-manager/"

		normalized := filepath.ToSlash(file)
		if idx := strings.Index(normalized, moduleRoot); idx >= 0 {
			return normalized[idx:]
		}

		return filepath.Base(file)
	}

	logger := log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
		NoColor:    true,
	}).With().Caller().Logger()

	return logger
}
