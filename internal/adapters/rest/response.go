package rest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var errResponseTooLarge = errors.New("response exceeds configured limit")

func readResponseBody(response *http.Response, limits Limits) ([]byte, error) {
	if response.ContentLength > limits.EncodedResponseBytes {
		return nil, errResponseTooLarge
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, limits.EncodedResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(encoded)) > limits.EncodedResponseBytes {
		return nil, errResponseTooLarge
	}
	var reader io.Reader = bytes.NewReader(encoded)
	switch strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding"))) {
	case "":
	case "gzip":
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("invalid gzip response")
		}
		defer compressed.Close()
		reader = compressed
	default:
		return nil, fmt.Errorf("unsupported content encoding")
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, limits.ResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(decoded)) > limits.ResponseBytes {
		return nil, errResponseTooLarge
	}
	return decoded, nil
}

func normalizeResponse(response *http.Response, body []byte, validator *jsonschema.Schema) (any, error) {
	if response.StatusCode == http.StatusNoContent {
		if validator != nil {
			if err := validator.Validate(nil); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		return nil, fmt.Errorf("missing or invalid content type")
	}
	var data any
	switch {
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&data); err != nil {
			return nil, fmt.Errorf("invalid JSON response")
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("multiple JSON values")
		}
	case mediaType == "text/plain":
		data = string(body)
	default:
		return nil, fmt.Errorf("unsupported response media type")
	}
	if validator != nil {
		if err := validator.Validate(data); err != nil {
			return nil, err
		}
	}
	return data, nil
}
