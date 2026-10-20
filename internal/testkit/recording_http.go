package testkit

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

type RecordedRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

type RecordingHTTP struct {
	Handler  http.Handler
	mu       sync.Mutex
	requests []RecordedRequest
}

func (recorder *RecordingHTTP) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	var err error
	if request.Body != nil {
		body, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		_ = request.Body.Close()
	}
	cloned := request.Clone(request.Context())
	cloned.Body = io.NopCloser(bytes.NewReader(body))
	recorder.mu.Lock()
	recorder.requests = append(recorder.requests, RecordedRequest{Method: request.Method, URL: request.URL.String(), Header: request.Header.Clone(), Body: append([]byte(nil), body...)})
	recorder.mu.Unlock()
	response := httptest.NewRecorder()
	handler := recorder.Handler
	if handler == nil {
		handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	}
	handler.ServeHTTP(response, cloned)
	result := response.Result()
	result.Request = request
	return result, nil
}

func (recorder *RecordingHTTP) Requests() []RecordedRequest {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	result := make([]RecordedRequest, len(recorder.requests))
	for index, request := range recorder.requests {
		result[index] = request
		result[index].Header = request.Header.Clone()
		result[index].Body = append([]byte(nil), request.Body...)
	}
	return result
}
