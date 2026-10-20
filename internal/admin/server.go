package admin

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
)

func New(address string, provider StatusProvider, metrics http.Handler) (http.Handler, error) {
	if provider == nil {
		return nil, errors.New("admin status provider is required")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("admin listen address must include host and port")
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("v1 admin listener must bind loopback")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("{\"live\":true}\n"))
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, _ *http.Request) {
		status := provider.Status()
		code := http.StatusOK
		if !status.Ready {
			code = http.StatusServiceUnavailable
		}
		writeJSON(writer, code, status)
	})
	mux.HandleFunc("GET /v1/status", func(writer http.ResponseWriter, _ *http.Request) { writeJSON(writer, http.StatusOK, provider.Status()) })
	mux.HandleFunc("GET /v1/upstreams", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, provider.Upstreams())
	})
	mux.HandleFunc("GET /v1/routes", func(writer http.ResponseWriter, _ *http.Request) { writeJSON(writer, http.StatusOK, provider.Routes()) })
	if metrics != nil {
		mux.Handle("GET /metrics", metrics)
	}
	return mux, nil
}
func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
