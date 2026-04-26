package envutil

import (
	"fmt"
	"log"
	"os"
	"strings"
)

func Require(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	log.Fatalf("Required environment variable %s is not set", key)
	return ""
}

func Get(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("required environment variable %s is not set", key)
}

func Bool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	}
	return def
}
