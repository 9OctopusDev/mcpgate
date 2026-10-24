package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/9OctopusDev/mcpgate/internal/catalog"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Limits struct {
	RequestBytes         int64
	EncodedResponseBytes int64
	ResponseBytes        int64
	OutputBytes          int64
}

func (limits Limits) defaults() Limits {
	if limits.RequestBytes <= 0 {
		limits.RequestBytes = 1 << 20
	}
	if limits.EncodedResponseBytes <= 0 {
		limits.EncodedResponseBytes = 4 << 20
	}
	if limits.ResponseBytes <= 0 {
		limits.ResponseBytes = 4 << 20
	}
	if limits.OutputBytes <= 0 {
		limits.OutputBytes = 4 << 20
	}
	return limits
}

type compiledRESTPlan struct {
	plan      *RESTPlan
	input     *jsonschema.Schema
	responses map[string]*jsonschema.Schema
}

type Adapter struct {
	plans      map[string]compiledRESTPlan
	client     *http.Client
	credential contracts.CredentialBinding
	limits     Limits
}

func NewAdapter(plans map[string]*RESTPlan, client *http.Client, credential contracts.CredentialBinding, limits Limits) (*Adapter, error) {
	if len(plans) == 0 || client == nil {
		return nil, fmt.Errorf("REST plans and HTTP client are required")
	}
	safeClient := *client
	safeClient.Jar = nil
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	adapter := &Adapter{plans: make(map[string]compiledRESTPlan, len(plans)), client: &safeClient, credential: credential, limits: limits.defaults()}
	for name, source := range plans {
		if source == nil {
			return nil, fmt.Errorf("REST plan %q is nil", name)
		}
		plan := cloneRESTPlan(source)
		input, err := compileValidator(plan.InputSchema, "input-"+name)
		if err != nil {
			return nil, fmt.Errorf("compile input schema: %w", err)
		}
		responses := make(map[string]*jsonschema.Schema, len(plan.ResponseSchemas))
		for status, schema := range plan.ResponseSchemas {
			validator, err := compileValidator(schema, "response-"+name+"-"+status)
			if err != nil {
				return nil, fmt.Errorf("compile response schema: %w", err)
			}
			responses[status] = validator
		}
		adapter.plans[name] = compiledRESTPlan{plan: plan, input: input, responses: responses}
	}
	return adapter, nil
}

func compileValidator(raw json.RawMessage, name string) (*jsonschema.Schema, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	uri := "https://mcpgate.invalid/rest/" + name + ".json"
	if err := compiler.AddResource(uri, resource); err != nil {
		return nil, err
	}
	return compiler.Compile(uri)
}

func (adapter *Adapter) CallTool(ctx context.Context, call contracts.Call, _ contracts.EventSink) (contracts.Outcome, error) {
	compiled, ok := adapter.plans[call.Route.NativeName]
	if !ok {
		return contracts.Outcome{}, fmt.Errorf("REST plan not found")
	}
	if int64(len(call.Arguments)) > adapter.limits.RequestBytes {
		return failureOutcome("INVALID_ARGUMENTS", "Tool arguments exceed the configured limit.", 0, "", "not_dispatched"), nil
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(call.Arguments))
	if err != nil || (compiled.input != nil && compiled.input.Validate(instance) != nil) {
		return failureOutcome("INVALID_ARGUMENTS", "Tool arguments do not match the published schema.", 0, "", "not_dispatched"), nil
	}
	arguments, err := decodeArguments(call.Arguments)
	if err != nil {
		return failureOutcome("INVALID_ARGUMENTS", "Tool arguments are malformed.", 0, "", "not_dispatched"), nil
	}
	requestContext := ctx
	var cancel context.CancelFunc
	if !call.Deadline.IsZero() {
		requestContext, cancel = context.WithDeadline(ctx, call.Deadline)
		defer cancel()
	}
	request, err := buildRequest(requestContext, compiled.plan, arguments, adapter.credential)
	if err != nil {
		if errors.Is(err, errCredentialApply) {
			return failureOutcome("UPSTREAM_AUTH_FAILED", "The configured upstream credential could not be applied.", 0, "", "not_dispatched"), nil
		}
		return failureOutcome("INVALID_ARGUMENTS", "Tool arguments could not be serialized safely.", 0, "", "not_dispatched"), nil
	}
	response, err := adapter.client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return failureOutcome("UPSTREAM_OUTCOME_UNKNOWN", "The request may have reached the service.", 0, call.ExecutionID, "unknown"), nil
	}
	defer response.Body.Close()
	body, err := readResponseBody(response, adapter.limits)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return failureOutcome("RESULT_TOO_LARGE", "The upstream result exceeds the configured limit.", response.StatusCode, call.ExecutionID, "completed"), nil
		}
		return failureOutcome("UPSTREAM_PROTOCOL_ERROR", "The upstream response could not be decoded safely.", response.StatusCode, call.ExecutionID, "completed"), nil
	}
	if response.StatusCode == http.StatusUnauthorized {
		return failureOutcome("UPSTREAM_AUTH_FAILED", "The upstream service rejected its configured credential.", response.StatusCode, "", "completed"), nil
	}
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusUnprocessableEntity {
		return failureOutcome("UPSTREAM_BUSINESS_ERROR", "The upstream service rejected the operation.", response.StatusCode, "", "completed"), nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return failureOutcome("UPSTREAM_UNAVAILABLE", "The upstream service did not complete the operation.", response.StatusCode, call.ExecutionID, "unknown"), nil
	}
	validator := compiled.responses[fmt.Sprintf("%d", response.StatusCode)]
	if validator == nil {
		validator = compiled.responses["default"]
	}
	data, err := normalizeResponse(response, body, validator)
	if err != nil {
		return failureOutcome("UPSTREAM_PROTOCOL_ERROR", "The upstream response violated its contract.", response.StatusCode, call.ExecutionID, "completed"), nil
	}
	envelope := successEnvelope{OK: true, Status: response.StatusCode, Data: data}
	encoded, _ := json.Marshal(envelope)
	if int64(len(encoded)*2) > adapter.limits.OutputBytes {
		return failureOutcome("RESULT_TOO_LARGE", "The normalized result exceeds the configured limit.", response.StatusCode, call.ExecutionID, "completed"), nil
	}
	return completeEnvelope(envelope, false), nil
}

func (adapter *Adapter) Discover(context.Context, contracts.CredentialBinding) (contracts.Catalog, error) {
	names := make([]string, 0, len(adapter.plans))
	for name := range adapter.plans {
		names = append(names, name)
	}
	sort.Strings(names)
	result := contracts.Catalog{Protocol: "rest"}
	for _, name := range names {
		plan := adapter.plans[name].plan
		contract := contracts.ToolContract{Name: name, InputSchema: append(json.RawMessage(nil), plan.InputSchema...)}
		contract.Fingerprint = catalog.Fingerprint(contract)
		result.Tools = append(result.Tools, contract)
	}
	return result, nil
}

func (adapter *Adapter) Close(context.Context) error {
	adapter.client.CloseIdleConnections()
	return nil
}

func cloneRESTPlan(source *RESTPlan) *RESTPlan {
	copy := *source
	copy.PathSegments = append([]PathSegment(nil), source.PathSegments...)
	copy.PathParameters = append([]ParameterPlan(nil), source.PathParameters...)
	copy.QueryParameters = append([]ParameterPlan(nil), source.QueryParameters...)
	for index := range copy.PathParameters {
		copy.PathParameters[index].Schema = append(json.RawMessage(nil), source.PathParameters[index].Schema...)
	}
	for index := range copy.QueryParameters {
		copy.QueryParameters[index].Schema = append(json.RawMessage(nil), source.QueryParameters[index].Schema...)
	}
	copy.InputSchema = append(json.RawMessage(nil), source.InputSchema...)
	copy.SecuritySchemes = append([]string(nil), source.SecuritySchemes...)
	copy.ResponseSchemas = make(map[string]json.RawMessage, len(source.ResponseSchemas))
	for status, schema := range source.ResponseSchemas {
		copy.ResponseSchemas[status] = append(json.RawMessage(nil), schema...)
	}
	if source.Body != nil {
		body := *source.Body
		body.Schema = append(json.RawMessage(nil), source.Body.Schema...)
		copy.Body = &body
	}
	return &copy
}

var _ contracts.ToolAdapter = (*Adapter)(nil)
