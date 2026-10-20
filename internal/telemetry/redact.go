package telemetry

import "strings"

var sensitiveKeys = []string{"authorization", "cookie", "token", "secret", "password", "arguments", "result", "session", "cursor", "stderr"}

func sensitive(key string) bool {
	key = strings.ToLower(key)
	for _, marker := range sensitiveKeys {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}
