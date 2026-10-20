package edge

import "strings"

func normalizeHost(host string) string {
	if index := strings.LastIndex(host, ":"); index >= 0 && !strings.Contains(host[index+1:], "]") {
		host = host[:index]
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

func allowedOrigin(origin string, allowed []string, required bool) bool {
	if origin == "" {
		return !required
	}
	for _, candidate := range allowed {
		if origin == candidate {
			return true
		}
	}
	return false
}
