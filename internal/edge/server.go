package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

type Verifier interface {
	Verify(context.Context, string, string) (contracts.Principal, error)
}

type Route struct {
	ServerID, Host, Path, ResourceURI string
	Origins                           []string
	RequireOrigin                     bool
	Handler                           http.Handler
}

type contextKey string

const principalKey contextKey = "mcpgate-principal"
const serverKey contextKey = "mcpgate-server"

func PrincipalFromContext(ctx context.Context) (contracts.Principal, bool) {
	principal, ok := ctx.Value(principalKey).(contracts.Principal)
	return principal, ok
}
func ServerFromContext(ctx context.Context) (string, bool) {
	server, ok := ctx.Value(serverKey).(string)
	return server, ok
}

func New(routes []Route, verifier Verifier, limits Limits) (http.Handler, error) {
	if verifier == nil || limits.HeaderBytes <= 0 || limits.BodyBytes <= 0 || limits.JSONDepth <= 0 || limits.ParseTimeout <= 0 {
		return nil, fmt.Errorf("invalid edge dependencies or limits")
	}
	compiled := make(map[string]Route, len(routes))
	for _, route := range routes {
		if route.ServerID == "" || route.Host == "" || route.Path == "" || route.Handler == nil {
			return nil, fmt.Errorf("invalid edge route")
		}
		key := normalizeHost(route.Host) + "\x00" + route.Path
		if _, exists := compiled[key]; exists {
			return nil, fmt.Errorf("duplicate edge route")
		}
		compiled[key] = route
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		route, found := compiled[normalizeHost(request.Host)+"\x00"+request.URL.Path]
		if !found || request.URL.RawPath != "" {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if headerBytes(request.Header)+int64(len(request.Host)) > limits.HeaderBytes {
			http.Error(writer, "request headers too large", http.StatusRequestHeaderFieldsTooLarge)
			return
		}
		if !allowedOrigin(request.Header.Get("Origin"), route.Origins, route.RequireOrigin) {
			http.Error(writer, "origin denied", http.StatusForbidden)
			return
		}
		parseCtx, cancel := context.WithTimeout(request.Context(), limits.ParseTimeout)
		defer cancel()
		body, err := readBody(parseCtx, request.Body, limits.BodyBytes)
		if err != nil {
			if errors.Is(err, errBodyLarge) {
				http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(writer, "invalid request", http.StatusBadRequest)
			}
			return
		}
		if err := validateUniqueJSON(body, limits.JSONDepth); err != nil {
			http.Error(writer, "invalid JSON request", http.StatusBadRequest)
			return
		}
		token, ok := bearer(request.Header.Get("Authorization"))
		if !ok {
			unauthorized(writer, route.ResourceURI)
			return
		}
		principal, err := verifier.Verify(request.Context(), token, route.ResourceURI)
		if err != nil {
			unauthorized(writer, route.ResourceURI)
			return
		}
		ctx := context.WithValue(request.Context(), principalKey, principal)
		ctx = context.WithValue(ctx, serverKey, route.ServerID)
		request = request.WithContext(ctx)
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
		route.Handler.ServeHTTP(writer, request)
	}), nil
}

var errBodyLarge = errors.New("body too large")

func readBody(ctx context.Context, body io.ReadCloser, limit int64) ([]byte, error) {
	if body == nil {
		return nil, fmt.Errorf("missing body")
	}
	defer body.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errBodyLarge
	}
	return data, nil
}

func headerBytes(headers http.Header) int64 {
	var size int64
	for key, values := range headers {
		size += int64(len(key))
		for _, value := range values {
			size += int64(len(value))
		}
	}
	return size
}
func bearer(value string) (string, bool) {
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
func unauthorized(writer http.ResponseWriter, resource string) {
	writer.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+resource+`/.well-known/oauth-protected-resource"`)
	http.Error(writer, "unauthorized", http.StatusUnauthorized)
}

func validateUniqueJSON(data []byte, maxDepth int) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanValue(decoder, 0, maxDepth); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func scanValue(decoder *json.Decoder, depth, maximum int) error {
	if depth > maximum {
		return fmt.Errorf("JSON nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON member")
			}
			seen[key] = struct{}{}
			if err := scanValue(decoder, depth+1, maximum); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanValue(decoder, depth+1, maximum); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != map[json.Delim]json.Delim{'{': '}', '[': ']'}[delimiter] {
		return fmt.Errorf("invalid JSON close")
	}
	return nil
}
