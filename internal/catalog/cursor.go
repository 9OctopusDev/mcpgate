package catalog

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

const (
	MaxCursorBytes        = 4096
	maxCursorPayloadBytes = 2048
	cursorVersion         = 1
)

var ErrInvalidCursor = errors.New("invalid catalog cursor")

type cursorPayload struct {
	Version   int    `json:"v"`
	ServerID  string `json:"server"`
	Partition string `json:"partition"`
	Revision  string `json:"revision"`
	LastName  string `json:"last"`
	ExpiresAt int64  `json:"exp"`
	KeyID     string `json:"kid"`
}

func cursorKeyID(key contracts.CredentialBinding) string {
	identity := key.Identity()
	return fmt.Sprintf("%s@%d", identity.Reference, identity.Epoch)
}

func encodeCursor(payload cursorPayload, key contracts.CredentialBinding) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) > maxCursorPayloadBytes {
		return "", ErrInvalidCursor
	}
	message := base64.RawURLEncoding.EncodeToString(encoded)
	signature, err := key.Sign([]byte(message))
	if err != nil {
		return "", fmt.Errorf("sign cursor: %w", err)
	}
	cursor := message + "." + base64.RawURLEncoding.EncodeToString(signature)
	if len(cursor) > MaxCursorBytes {
		return "", ErrInvalidCursor
	}
	return cursor, nil
}

func decodeCursor(value string, key contracts.CredentialBinding, serverID, partition, revision string, now time.Time) (cursorPayload, error) {
	if value == "" || len(value) > MaxCursorBytes || strings.Count(value, ".") != 1 {
		return cursorPayload{}, ErrInvalidCursor
	}
	parts := strings.SplitN(value, ".", 2)
	payloadBytes, payloadErr := base64.RawURLEncoding.DecodeString(parts[0])
	signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[1])
	if payloadErr != nil || signatureErr != nil || len(payloadBytes) > maxCursorPayloadBytes || key.Verify([]byte(parts[0]), signature) != nil {
		return cursorPayload{}, ErrInvalidCursor
	}
	var payload cursorPayload
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return cursorPayload{}, ErrInvalidCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return cursorPayload{}, ErrInvalidCursor
	}
	if payload.Version != cursorVersion || payload.ServerID != serverID || payload.Partition != partition || payload.Revision != revision || payload.KeyID != cursorKeyID(key) || payload.LastName == "" || payload.ExpiresAt <= now.Unix() {
		return cursorPayload{}, ErrInvalidCursor
	}
	return payload, nil
}
