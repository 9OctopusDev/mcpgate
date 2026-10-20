package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

var (
	ErrPolicyChanged = errors.New("authorization policy changed while building catalog")
	ErrListInvalid   = errors.New("invalid catalog list request")
)

type EpochAuthorizer interface {
	contracts.Authorizer
	Epoch() uint64
}

type ListRequest struct {
	ServerID          string
	Principal         contracts.Principal
	Routes            []contracts.Route
	Authorizer        EpochAuthorizer
	Revision          string
	PageSize          int
	Cursor            string
	CursorKey         contracts.CredentialBinding
	CursorTTL         time.Duration
	Now               func() time.Time
	SupportedFeatures map[string]bool
	Available         func(contracts.Route) bool
}

type ListPage struct {
	Tools       []contracts.ToolContract `json:"tools"`
	NextCursor  string                   `json:"nextCursor,omitempty"`
	TTLMillis   int                      `json:"ttlMs"`
	CacheScope  string                   `json:"cacheScope"`
	PolicyEpoch uint64                   `json:"-"`
}

func List(ctx context.Context, request ListRequest) (ListPage, error) {
	if request.ServerID == "" || request.Revision == "" || request.PageSize <= 0 || request.Authorizer == nil || request.CursorKey == nil || request.Principal.PartitionDigest == "" {
		return ListPage{}, ErrListInvalid
	}
	if request.Now == nil {
		request.Now = time.Now
	}
	if request.CursorTTL <= 0 {
		request.CursorTTL = 5 * time.Minute
	}
	for attempt := 0; attempt < 2; attempt++ {
		page, changed, err := listAtEpoch(ctx, request)
		if err != nil {
			return ListPage{}, err
		}
		if !changed {
			return page, nil
		}
	}
	return ListPage{}, ErrPolicyChanged
}

func listAtEpoch(ctx context.Context, request ListRequest) (ListPage, bool, error) {
	startEpoch := request.Authorizer.Epoch()
	visible := make([]contracts.ToolContract, 0, len(request.Routes))
	for _, route := range request.Routes {
		if err := ctx.Err(); err != nil {
			return ListPage{}, false, err
		}
		if route.Key.ServerID != request.ServerID || (request.Available != nil && !request.Available(route)) || !featuresSupported(route.Contract.RequiredFeatures, request.SupportedFeatures) {
			continue
		}
		decision, err := request.Authorizer.Authorize(ctx, request.Principal, route)
		if err != nil {
			return ListPage{}, false, err
		}
		if decision.Epoch != startEpoch || request.Authorizer.Epoch() != startEpoch {
			return ListPage{}, true, nil
		}
		if decision.Allowed {
			contract := cloneContract(route.Contract)
			contract.Name = route.Key.PublicName
			visible = append(visible, contract)
		}
	}
	if request.Authorizer.Epoch() != startEpoch {
		return ListPage{}, true, nil
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].Name < visible[j].Name })

	start := 0
	if request.Cursor != "" {
		payload, err := decodeCursor(request.Cursor, request.CursorKey, request.ServerID, request.Principal.PartitionDigest, request.Revision, request.Now())
		if err != nil {
			return ListPage{}, false, err
		}
		start = sort.Search(len(visible), func(index int) bool { return visible[index].Name > payload.LastName })
	}
	end := start + request.PageSize
	if end > len(visible) {
		end = len(visible)
	}
	page := ListPage{Tools: append([]contracts.ToolContract(nil), visible[start:end]...), TTLMillis: 0, CacheScope: "private", PolicyEpoch: startEpoch}
	if end < len(visible) {
		cursor, err := encodeCursor(cursorPayload{
			Version: cursorVersion, ServerID: request.ServerID, Partition: request.Principal.PartitionDigest,
			Revision: request.Revision, LastName: visible[end-1].Name, ExpiresAt: request.Now().Add(request.CursorTTL).Unix(), KeyID: cursorKeyID(request.CursorKey),
		}, request.CursorKey)
		if err != nil {
			return ListPage{}, false, err
		}
		page.NextCursor = cursor
	}
	if request.Authorizer.Epoch() != startEpoch {
		return ListPage{}, true, nil
	}
	return page, false, nil
}

func featuresSupported(required []string, supported map[string]bool) bool {
	for _, feature := range required {
		if !supported[feature] {
			return false
		}
	}
	return true
}

func cloneContract(contract contracts.ToolContract) contracts.ToolContract {
	contract.InputSchema = append(json.RawMessage(nil), contract.InputSchema...)
	contract.OutputSchema = append(json.RawMessage(nil), contract.OutputSchema...)
	contract.RequiredFeatures = append([]string(nil), contract.RequiredFeatures...)
	return contract
}
