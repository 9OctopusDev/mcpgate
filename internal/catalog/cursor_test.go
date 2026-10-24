package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

func firstCursor(t *testing.T) (ListRequest, string) {
	t.Helper()
	authorizer := &catalogAuthorizer{}
	request := baseListRequest(authorizer)
	request.PageSize = 1
	page, err := List(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == "" {
		t.Fatal("missing cursor")
	}
	return request, page.NextCursor
}

func TestCursorRejectsTamperingExpiryAndBindingChanges(t *testing.T) {
	base, cursor := firstCursor(t)
	tests := []struct {
		name string
		edit func(*ListRequest)
	}{
		{name: "tampered", edit: func(request *ListRequest) { request.Cursor = cursor[:len(cursor)-1] + "A" }},
		{name: "expired", edit: func(request *ListRequest) {
			request.Cursor = cursor
			request.Now = func() time.Time { return time.Unix(2_000_001_000, 0) }
		}},
		{name: "cross tenant", edit: func(request *ListRequest) { request.Cursor = cursor; request.Principal.PartitionDigest = "partition-b" }},
		{name: "cross server", edit: func(request *ListRequest) { request.Cursor = cursor; request.ServerID = "admin" }},
		{name: "old revision", edit: func(request *ListRequest) { request.Cursor = cursor; request.Revision = "catalog-rev-2" }},
		{name: "rotated key", edit: func(request *ListRequest) {
			request.Cursor = cursor
			request.CursorKey = &testSigningKey{identity: contracts.CredentialIdentity{Reference: "env:CURSOR", Epoch: 5}, key: []byte("01234567890123456789012345678901")}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			test.edit(&request)
			if _, err := List(context.Background(), request); !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("expected invalid cursor, got %v", err)
			}
		})
	}
}

func TestCursorRejectsOversizedInput(t *testing.T) {
	authorizer := &catalogAuthorizer{}
	request := baseListRequest(authorizer)
	request.Cursor = strings.Repeat("x", MaxCursorBytes+1)
	if _, err := List(context.Background(), request); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("oversized cursor accepted: %v", err)
	}
}
