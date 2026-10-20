package credentials

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

var (
	ErrBindingClosed    = errors.New("credential binding is closed")
	ErrSignatureInvalid = errors.New("credential signature is invalid")
)

type Binding struct {
	mu       sync.RWMutex
	identity contracts.CredentialIdentity
	provider string
	name     string
	secret   []byte
	signing  bool
	closed   bool
}

func (b *Binding) Identity() contracts.CredentialIdentity {
	return b.identity
}

func (b *Binding) ApplyHTTP(request *http.Request) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrBindingClosed
	}
	if b.signing {
		return contracts.ErrCredentialOperationUnsupported
	}
	request.Header.Set("Authorization", "Bearer "+string(b.secret))
	return nil
}

func (b *Binding) Sign(message []byte) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return nil, ErrBindingClosed
	}
	if !b.signing {
		return nil, contracts.ErrCredentialOperationUnsupported
	}
	mac := hmac.New(sha256.New, b.secret)
	_, _ = mac.Write(message)
	return mac.Sum(nil), nil
}

func (b *Binding) Verify(message, signature []byte) error {
	expected, err := b.Sign(message)
	if err != nil {
		return err
	}
	if !hmac.Equal(expected, signature) {
		return ErrSignatureInvalid
	}
	return nil
}

func (b *Binding) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	zero(b.secret)
	b.secret = nil
	b.closed = true
}

func (b *Binding) String() string {
	return fmt.Sprintf("credential(%s:%s epoch=%d)", b.provider, b.name, b.identity.Epoch)
}

func (b *Binding) GoString() string { return b.String() }

func (b *Binding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Reference string `json:"reference"`
		Epoch     uint64 `json:"epoch"`
	}{Reference: b.identity.Reference, Epoch: b.identity.Epoch})
}
