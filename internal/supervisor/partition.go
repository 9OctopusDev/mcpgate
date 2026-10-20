package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

type PartitionKey struct {
	Generation, Tenant, Principal, Session string
	CredentialEpoch                        uint64
}

func (key PartitionKey) String() string {
	parts := []string{key.Generation, key.Tenant, key.Principal, key.Session, strconv.FormatUint(key.CredentialEpoch, 10)}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
