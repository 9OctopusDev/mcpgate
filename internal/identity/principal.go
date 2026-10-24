package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

func makePrincipal(key []byte, issuer, subject, tenant, clientID string, scopes []string) contracts.Principal {
	normalized := append([]string(nil), scopes...)
	sort.Strings(normalized)
	digest := hmac.New(sha256.New, key)
	_, _ = digest.Write([]byte(strings.Join([]string{issuer, subject, tenant, clientID}, "\x00")))
	return contracts.Principal{
		Issuer: issuer, Subject: subject, Tenant: tenant, ClientID: clientID,
		Scopes: normalized, PartitionDigest: hex.EncodeToString(digest.Sum(nil)),
	}
}
