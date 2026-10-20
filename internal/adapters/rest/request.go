package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var errCredentialApply = errors.New("apply upstream credential")

type requestArguments struct {
	path  map[string]any
	query map[string]any
	body  json.RawMessage
}

type oneShotReader struct{ io.Reader }

func decodeArguments(raw json.RawMessage) (requestArguments, error) {
	var groups map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&groups); err != nil {
		return requestArguments{}, err
	}
	result := requestArguments{path: make(map[string]any), query: make(map[string]any)}
	for name, value := range groups {
		switch name {
		case "path":
			if err := decodeNumberMap(value, &result.path); err != nil {
				return requestArguments{}, err
			}
		case "query":
			if err := decodeNumberMap(value, &result.query); err != nil {
				return requestArguments{}, err
			}
		case "body":
			result.body = append(json.RawMessage(nil), value...)
		default:
			return requestArguments{}, fmt.Errorf("unknown argument group")
		}
	}
	return result, nil
}

func decodeNumberMap(raw json.RawMessage, target *map[string]any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func buildRequest(ctx context.Context, plan *RESTPlan, arguments requestArguments, credential contractsCredential) (*http.Request, error) {
	path, err := plan.BuildPath(arguments.path)
	if err != nil {
		return nil, err
	}
	query, err := plan.BuildQuery(arguments.query)
	if err != nil {
		return nil, err
	}
	requestURL := plan.EndpointBase + path
	if query != "" {
		requestURL += "?" + query
	}
	var body io.Reader
	if arguments.body != nil {
		body = oneShotReader{Reader: bytes.NewReader(arguments.body)}
	}
	request, err := http.NewRequestWithContext(ctx, plan.Method, requestURL, body)
	if err != nil {
		return nil, err
	}
	if arguments.body != nil {
		request.ContentLength = int64(len(arguments.body))
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Accept", "application/json, text/plain")
	if credential != nil {
		if err := credential.ApplyHTTP(request); err != nil {
			return nil, errCredentialApply
		}
	}
	return request, nil
}

type contractsCredential interface{ ApplyHTTP(*http.Request) error }
