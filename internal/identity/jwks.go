package identity

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
)

const maxJWKSBytes = 1 << 20

var ErrIdentityVerification = errors.New("identity verification failed")

type jwkDocument struct {
	Keys []struct {
		KeyType string `json:"kty"`
		Use     string `json:"use"`
		Alg     string `json:"alg"`
		KeyID   string `json:"kid"`
		N       string `json:"n"`
		E       string `json:"e"`
	} `json:"keys"`
}

func fetchKeys(ctx context.Context, client *http.Client, uri string) (map[string]*rsa.PublicKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid JWKS URI", ErrIdentityVerification)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: JWKS unavailable", ErrIdentityVerification)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: JWKS status %d", ErrIdentityVerification, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxJWKSBytes+1))
	if err != nil || len(body) > maxJWKSBytes {
		return nil, fmt.Errorf("%w: invalid JWKS body", ErrIdentityVerification)
	}
	var document jwkDocument
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || len(document.Keys) == 0 {
		return nil, fmt.Errorf("%w: malformed JWKS", ErrIdentityVerification)
	}
	keys := make(map[string]*rsa.PublicKey, len(document.Keys))
	for _, candidate := range document.Keys {
		if candidate.KeyType != "RSA" || candidate.Use != "sig" || candidate.Alg != "RS256" || candidate.KeyID == "" {
			continue
		}
		modulus, nerr := base64.RawURLEncoding.DecodeString(candidate.N)
		exponent, eerr := base64.RawURLEncoding.DecodeString(candidate.E)
		if nerr != nil || eerr != nil || len(modulus) < 256 || len(exponent) == 0 || len(exponent) > 4 {
			continue
		}
		e := 0
		for _, octet := range exponent {
			e = e<<8 | int(octet)
		}
		if e < 3 || e%2 == 0 {
			continue
		}
		if _, exists := keys[candidate.KeyID]; exists {
			return nil, fmt.Errorf("%w: duplicate JWKS key id", ErrIdentityVerification)
		}
		keys[candidate.KeyID] = &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: e}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: no usable JWKS signing keys", ErrIdentityVerification)
	}
	return keys, nil
}
