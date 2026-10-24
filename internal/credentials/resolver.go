package credentials

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"github.com/9OctopusDev/mcpgate/internal/config"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

var nextEpoch atomic.Uint64

const maxSecretBytes = 1 << 20

var (
	ErrUnsupportedProvider = errors.New("unsupported secret provider")
	ErrSecretUnavailable   = errors.New("secret is unavailable")
	ErrSecretPermissions   = errors.New("secret file permissions are too broad")
	ErrWeakCursorKey       = errors.New("cursor signing key must contain at least 32 decoded bytes")
)

func Resolve(ctx context.Context, ref config.SecretRef) (contracts.CredentialBinding, error) {
	material, provider, name, err := resolveBytes(ctx, ref)
	if err != nil {
		return nil, err
	}
	return newBinding(provider, name, material, false), nil
}

func ResolveCursorKey(ctx context.Context, ref config.SecretRef) (contracts.CredentialBinding, error) {
	encoded, provider, name, err := resolveBytes(ctx, ref)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	zero(encoded)
	if err != nil || len(decoded) < 32 {
		zero(decoded)
		return nil, ErrWeakCursorKey
	}
	return newBinding(provider, name, decoded, true), nil
}

func resolveBytes(ctx context.Context, ref config.SecretRef) ([]byte, string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", "", err
	}
	switch ref.Source {
	case "env":
		if ref.Name == "" {
			return nil, "", "", fmt.Errorf("%w: env name is empty", ErrSecretUnavailable)
		}
		value, ok := os.LookupEnv(ref.Name)
		if !ok || value == "" {
			return nil, "", "", fmt.Errorf("%w: env reference %q", ErrSecretUnavailable, ref.Name)
		}
		return []byte(value), "env", ref.Name, nil
	case "file":
		if ref.Path == "" {
			return nil, "", "", fmt.Errorf("%w: file path is empty", ErrSecretUnavailable)
		}
		file, err := os.Open(ref.Path)
		if err != nil {
			return nil, "", "", fmt.Errorf("%w: file reference", ErrSecretUnavailable)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, "", "", fmt.Errorf("%w: file reference", ErrSecretUnavailable)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return nil, "", "", ErrSecretPermissions
		}
		content, err := io.ReadAll(io.LimitReader(file, maxSecretBytes+1))
		if err != nil || len(content) == 0 || len(content) > maxSecretBytes {
			return nil, "", "", fmt.Errorf("%w: file reference", ErrSecretUnavailable)
		}
		content = bytes.TrimSuffix(content, []byte("\n"))
		content = bytes.TrimSuffix(content, []byte("\r"))
		return append([]byte(nil), content...), "file", ref.Path, nil
	default:
		return nil, "", "", ErrUnsupportedProvider
	}
}

func newBinding(provider, name string, secret []byte, signing bool) *Binding {
	return &Binding{
		identity: contracts.CredentialIdentity{
			Reference: provider + ":" + name,
			Epoch:     nextEpoch.Add(1),
		},
		provider: provider,
		name:     name,
		secret:   append([]byte(nil), secret...),
		signing:  signing,
	}
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
