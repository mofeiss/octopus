package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/protocolcompat"
)

// ResponseOutbound implements the Outbound interface for OpenAI Responses API.
type ResponseOutbound struct {
	// Stream state tracking
	streamID    string
	streamModel string
	initialized bool

	// [fork] Track streamed tool-call fragments so we only emit one complete
	// tool invocation per call_id. Some Responses upstreams send arguments in
	// deltas and then repeat the full JSON again in done/completed events.
	toolCallState map[string]*responsesToolCallStreamState

	// [fork] Use the sub2api-compatible Responses -> Chat stream state machine as
	// the primary parser for cross-protocol relay paths.
	responsesChatState *protocolcompat.ResponsesEventToChatState
}

type responsesToolCallStreamState struct {
	hasArgumentDelta bool
	completedEmitted bool
}

func (o *ResponseOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}

	body, err := marshalResponsesRequest(request)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	// Parse and set URL
	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedUrl.Path = parsedUrl.Path + "/responses"
	req.URL = parsedUrl
	req.Method = http.MethodPost

	return req, nil
}

func marshalResponsesRequest(request *model.InternalLLMRequest) ([]byte, error) {
	if preservedBody, ok, err := tryPreserveRawResponsesRequest(request); err != nil {
		return nil, err
	} else if ok {
		return preservedBody, nil
	}
	if compatBody, ok, err := tryMarshalCompatResponsesRequest(request); err != nil {
		return nil, err
	} else if ok {
		return compatBody, nil
	}

	// Convert to Responses API request format
	responsesReq := ConvertToResponsesRequest(request)

	body, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal responses api request: %w", err)
	}

	return body, nil
}

// tryPreserveRawResponsesRequest keeps the original Responses payload shape when the
// request entered Octopus as OpenAI Responses and is being forwarded to a
// Responses-compatible upstream. This avoids losing fields that the internal
// chat-shaped model does not round-trip, which is important for upstream cache hits.
func tryPreserveRawResponsesRequest(request *model.InternalLLMRequest) ([]byte, bool, error) {
	if request == nil || request.RawAPIFormat != model.APIFormatOpenAIResponse || len(request.RawRequest) == 0 {
		return nil, false, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(request.RawRequest, &raw); err != nil {
		return nil, false, fmt.Errorf("failed to decode raw responses api request: %w", err)
	}

	modelValue, err := json.Marshal(request.Model)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal response request model: %w", err)
	}
	raw["model"] = modelValue

	body, err := json.Marshal(raw)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal preserved responses api request: %w", err)
	}

	return body, true, nil
}

// [fork] When the inbound protocol differs from the selected upstream
// Responses-compatible channel, reuse the sub2api request conversion logic
// instead of relying on Octopus' legacy ad-hoc transform.
func tryMarshalCompatResponsesRequest(request *model.InternalLLMRequest) ([]byte, bool, error) {
	compatReq, ok, err := buildCompatResponsesRequest(request)
	if err != nil || !ok {
		return nil, ok, err
	}

	body, err := json.Marshal(compatReq)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal compat responses request: %w", err)
	}

	return body, true, nil
}

func buildCompatResponsesRequest(request *model.InternalLLMRequest) (*protocolcompat.ResponsesRequest, bool, error) {
	if request == nil || len(request.RawRequest) == 0 {
		return nil, false, nil
	}

	switch request.RawAPIFormat {
	case model.APIFormatOpenAIChatCompletion:
		var raw protocolcompat.ChatCompletionsRequest
		if err := json.Unmarshal(request.RawRequest, &raw); err != nil {
			return nil, false, fmt.Errorf("failed to decode raw chat completions request: %w", err)
		}

		compatReq, err := protocolcompat.ChatCompletionsToResponses(&raw)
		if err != nil {
			return nil, false, fmt.Errorf("failed to convert raw chat completions request to responses request: %w", err)
		}
		compatReq.Model = request.Model
		compatReq.Stream = request.Stream != nil && *request.Stream
		return compatReq, true, nil
	case model.APIFormatAnthropicMessage:
		var raw protocolcompat.AnthropicRequest
		if err := json.Unmarshal(request.RawRequest, &raw); err != nil {
			return nil, false, fmt.Errorf("failed to decode raw anthropic request: %w", err)
		}

		compatReq, err := protocolcompat.AnthropicToResponses(&raw)
		if err != nil {
			return nil, false, fmt.Errorf("failed to convert raw anthropic request to responses request: %w", err)
		}
		compatReq.Model = request.Model
		compatReq.Stream = request.Stream != nil && *request.Stream
		return compatReq, true, nil
	default:
		return nil, false, nil
	}
}

func (o *ResponseOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	if response == nil {
		return nil, fmt.Errorf("response is nil")
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("response body is empty")
	}

	// Check for error response
	if response.StatusCode >= 400 {
		var errResp struct {
			Error model.ErrorDetail `json:"error"`
		}
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, &model.ResponseError{
				StatusCode: response.StatusCode,
				Detail:     errResp.Error,
			}
		}
		return nil, fmt.Errorf("HTTP error %d: %s", response.StatusCode, string(body))
	}

	var resp ResponsesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		var compatResp protocolcompat.ResponsesResponse
		if compatErr := json.Unmarshal(body, &compatResp); compatErr != nil {
			return nil, fmt.Errorf("failed to unmarshal responses api response: %w", err)
		}

		chatResp := protocolcompat.ResponsesToChatCompletions(&compatResp, compatResp.Model)
		internalResp, bridgeErr := protocolcompat.ChatResponseToInternal(chatResp)
		if bridgeErr != nil {
			return nil, fmt.Errorf("failed to bridge compat responses api response: %w", bridgeErr)
		}
		return internalResp, nil
	}

	// [fork] Prefer the sub2api-compatible bridge first, but keep the legacy
	// converter as a safe fallback for provider-specific shape drift.
	compatResp := protocolcompat.ResponsesResponse{
		ID:     resp.ID,
		Object: resp.Object,
		Model:  resp.Model,
		Output: make([]protocolcompat.ResponsesOutput, 0, len(resp.Output)),
	}
	if resp.Status != nil {
		compatResp.Status = *resp.Status
	}
	if resp.Error != nil {
		compatResp.Error = &protocolcompat.ResponsesError{
			Message: resp.Error.Message,
		}
	}
	if resp.Usage != nil {
		compatResp.Usage = convertLegacyResponsesUsageToCompat(resp.Usage)
	}
	for _, item := range resp.Output {
		compatResp.Output = append(compatResp.Output, convertLegacyResponsesItemToCompat(item))
	}

	chatResp := protocolcompat.ResponsesToChatCompletions(&compatResp, compatResp.Model)
	internalResp, bridgeErr := protocolcompat.ChatResponseToInternal(chatResp)
	if bridgeErr == nil {
		return internalResp, nil
	}

	// Convert to internal response
	return convertToLLMResponseFromResponses(&resp), nil
}

func (o *ResponseOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	if len(eventData) == 0 {
		return nil, nil
	}

	// Handle [DONE] marker
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return &model.InternalLLMResponse{
			Object: "[DONE]",
		}, nil
	}

	var errCheck struct {
		Error *model.ErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(eventData, &errCheck); err == nil && errCheck.Error != nil {
		return nil, &model.ResponseError{
			Detail: *errCheck.Error,
		}
	}

	// Initialize state if needed
	if !o.initialized {
		o.initialized = true
	}
	if o.toolCallState == nil {
		o.toolCallState = make(map[string]*responsesToolCallStreamState)
	}
	if o.responsesChatState == nil {
		o.responsesChatState = protocolcompat.NewResponsesEventToChatState()
		o.responsesChatState.IncludeUsage = true
	}

	// [fork] Parse the streaming event with the sub2api-compatible state machine
	// first, then keep the legacy tool-call completion fallbacks for providers
	// that only emit *.done / completed tool payloads.
	var compatEvent protocolcompat.ResponsesStreamEvent
	if err := json.Unmarshal(eventData, &compatEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream event: %w", err)
	}

	o.trackCompatResponsesToolState(&compatEvent)

	compatChunks := protocolcompat.ResponsesEventToChatChunks(&compatEvent, o.responsesChatState)
	resp := mergeCompatChatChunks(compatChunks)
	var err error
	var internalResp *model.InternalLLMResponse
	if resp != nil {
		internalResp, err = protocolcompat.ChatChunkToInternal(resp)
		if err != nil {
			return nil, fmt.Errorf("failed to bridge compat responses stream chunk: %w", err)
		}
	}

	internalResp = o.applyCompatResponsesStreamFallback(&compatEvent, internalResp)
	if internalResp == nil {
		return nil, nil
	}
	if compatEvent.Response != nil {
		if compatEvent.Response.ID != "" {
			internalResp.ID = compatEvent.Response.ID
			o.streamID = compatEvent.Response.ID
		}
		if compatEvent.Response.Model != "" {
			internalResp.Model = compatEvent.Response.Model
			o.streamModel = compatEvent.Response.Model
		}
	}

	if compatEvent.Type == "response.failed" || compatEvent.Type == "error" ||
		(compatEvent.Response != nil && compatEvent.Response.Status == "failed") {
		ensureInternalResponseChoice(internalResp)
		internalResp.Choices[0].FinishReason = lo.ToPtr("error")
	}

	return internalResp, nil
}

func (o *ResponseOutbound) getToolCallStreamState(callID string) *responsesToolCallStreamState {
	if o.toolCallState == nil {
		o.toolCallState = make(map[string]*responsesToolCallStreamState)
	}
	state, ok := o.toolCallState[callID]
	if !ok {
		state = &responsesToolCallStreamState{}
		o.toolCallState[callID] = state
	}
	return state
}

func (o *ResponseOutbound) buildPendingToolCallsFromResponsesItems(items []ResponsesItem) []model.ToolCall {
	toolCalls := make([]model.ToolCall, 0)
	for idx, item := range items {
		if item.Type != "function_call" {
			continue
		}
		state := o.getToolCallStreamState(item.CallID)
		if state.completedEmitted || state.hasArgumentDelta {
			continue
		}
		state.completedEmitted = true
		toolCalls = append(toolCalls, model.ToolCall{
			Index: idx,
			ID:    item.CallID,
			Type:  "function",
			Function: model.FunctionCall{
				Name:      item.Name,
				Arguments: item.Arguments,
			},
		})
	}
	return toolCalls
}

func (o *ResponseOutbound) trackCompatResponsesToolState(event *protocolcompat.ResponsesStreamEvent) {
	if event == nil {
		return
	}

	switch event.Type {
	case "response.output_item.added":
		if event.Item != nil && event.Item.Type == "function_call" {
			o.getToolCallStreamState(event.Item.CallID)
		}
	case "response.function_call_arguments.delta":
		if event.CallID == "" {
			return
		}
		state := o.getToolCallStreamState(event.CallID)
		state.hasArgumentDelta = state.hasArgumentDelta || event.Delta != ""
	}
}

func (o *ResponseOutbound) applyCompatResponsesStreamFallback(event *protocolcompat.ResponsesStreamEvent, resp *model.InternalLLMResponse) *model.InternalLLMResponse {
	if event == nil {
		return resp
	}

	switch event.Type {
	case "response.function_call_arguments.done":
		if event.CallID == "" && event.Arguments == "" {
			return resp
		}

		state := o.getToolCallStreamState(event.CallID)
		if state.completedEmitted || state.hasArgumentDelta {
			return resp
		}
		state.completedEmitted = true

		resp = ensureCompatInternalChunk(resp, o.streamID, o.streamModel)
		ensureInternalResponseChoice(resp)
		resp.Choices[0].Delta = &model.Message{
			Role: "assistant",
			ToolCalls: []model.ToolCall{{
				Index: event.OutputIndex,
				ID:    event.CallID,
				Type:  "function",
				Function: model.FunctionCall{
					Name:      event.Name,
					Arguments: event.Arguments,
				},
			}},
		}
	case "response.output_item.done":
		if event.Item == nil || event.Item.Type != "function_call" {
			return resp
		}

		state := o.getToolCallStreamState(event.Item.CallID)
		if state.completedEmitted || state.hasArgumentDelta {
			return resp
		}
		state.completedEmitted = true

		resp = ensureCompatInternalChunk(resp, o.streamID, o.streamModel)
		ensureInternalResponseChoice(resp)
		resp.Choices[0].Delta = &model.Message{
			Role: "assistant",
			ToolCalls: []model.ToolCall{{
				Index: event.OutputIndex,
				ID:    event.Item.CallID,
				Type:  "function",
				Function: model.FunctionCall{
					Name:      event.Item.Name,
					Arguments: event.Item.Arguments,
				},
			}},
		}
	case "response.completed":
		if event.Response == nil {
			return resp
		}

		o.streamID = lo.Ternary(event.Response.ID != "", event.Response.ID, o.streamID)
		o.streamModel = lo.Ternary(event.Response.Model != "", event.Response.Model, o.streamModel)
		resp = ensureCompatInternalChunk(resp, o.streamID, o.streamModel)
		if resp.Usage == nil && event.Response.Usage != nil {
			resp.Usage = convertCompatResponsesUsage(event.Response.Usage)
		}

		toolCalls := o.buildPendingToolCallsFromCompatOutputs(event.Response.Output)
		if len(toolCalls) == 0 {
			return resp
		}

		ensureInternalResponseChoice(resp)
		if resp.Choices[0].Delta == nil {
			resp.Choices[0].Delta = &model.Message{Role: "assistant"}
		}
		resp.Choices[0].Delta.Role = "assistant"
		resp.Choices[0].Delta.ToolCalls = append(resp.Choices[0].Delta.ToolCalls, toolCalls...)
		resp.Choices[0].FinishReason = lo.ToPtr("tool_calls")
	}

	return resp
}

func (o *ResponseOutbound) buildPendingToolCallsFromCompatOutputs(items []protocolcompat.ResponsesOutput) []model.ToolCall {
	toolCalls := make([]model.ToolCall, 0)
	for idx, item := range items {
		if item.Type != "function_call" {
			continue
		}
		state := o.getToolCallStreamState(item.CallID)
		if state.completedEmitted || state.hasArgumentDelta {
			continue
		}
		state.completedEmitted = true
		toolCalls = append(toolCalls, model.ToolCall{
			Index: idx,
			ID:    item.CallID,
			Type:  "function",
			Function: model.FunctionCall{
				Name:      item.Name,
				Arguments: item.Arguments,
			},
		})
	}
	return toolCalls
}

func mergeCompatChatChunks(chunks []protocolcompat.ChatCompletionsChunk) *protocolcompat.ChatCompletionsChunk {
	if len(chunks) == 0 {
		return nil
	}

	merged := &protocolcompat.ChatCompletionsChunk{}
	for _, chunk := range chunks {
		if merged.ID == "" {
			merged.ID = chunk.ID
		}
		if merged.Object == "" {
			merged.Object = chunk.Object
		}
		if merged.Created == 0 {
			merged.Created = chunk.Created
		}
		if merged.Model == "" {
			merged.Model = chunk.Model
		}
		if merged.SystemFingerprint == "" {
			merged.SystemFingerprint = chunk.SystemFingerprint
		}
		if merged.ServiceTier == "" {
			merged.ServiceTier = chunk.ServiceTier
		}
		if chunk.Usage != nil {
			merged.Usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if len(merged.Choices) == 0 {
			merged.Choices = append(merged.Choices, chunk.Choices[0])
			continue
		}

		dst := &merged.Choices[0]
		src := chunk.Choices[0]
		if dst.Delta.Role == "" {
			dst.Delta.Role = src.Delta.Role
		}
		if src.Delta.Content != nil && (dst.Delta.Content == nil || *src.Delta.Content != "") {
			dst.Delta.Content = src.Delta.Content
		}
		if src.Delta.ReasoningContent != nil {
			dst.Delta.ReasoningContent = src.Delta.ReasoningContent
		}
		if len(src.Delta.ToolCalls) > 0 {
			dst.Delta.ToolCalls = append(dst.Delta.ToolCalls, src.Delta.ToolCalls...)
		}
		if src.FinishReason != nil {
			dst.FinishReason = src.FinishReason
		}
	}

	if merged.Object == "" {
		merged.Object = "chat.completion.chunk"
	}

	return merged
}

func ensureCompatInternalChunk(resp *model.InternalLLMResponse, id, modelName string) *model.InternalLLMResponse {
	if resp == nil {
		resp = &model.InternalLLMResponse{
			Object: "chat.completion.chunk",
		}
	}
	if resp.ID == "" {
		resp.ID = id
	}
	if resp.Model == "" {
		resp.Model = modelName
	}
	return resp
}

func ensureInternalResponseChoice(resp *model.InternalLLMResponse) {
	if resp == nil {
		return
	}
	if len(resp.Choices) == 0 {
		resp.Choices = []model.Choice{{Index: 0}}
	}
}

func convertCompatResponsesUsage(usage *protocolcompat.ResponsesUsage) *model.Usage {
	if usage == nil {
		return nil
	}

	result := &model.Usage{
		PromptTokens:     int64(usage.InputTokens),
		CompletionTokens: int64(usage.OutputTokens),
		TotalTokens:      int64(usage.TotalTokens),
	}
	if usage.InputTokensDetails != nil && usage.InputTokensDetails.CachedTokens > 0 {
		result.PromptTokensDetails = &model.PromptTokensDetails{
			CachedTokens: int64(usage.InputTokensDetails.CachedTokens),
		}
	}
	if usage.OutputTokensDetails != nil && usage.OutputTokensDetails.ReasoningTokens > 0 {
		result.CompletionTokensDetails = &model.CompletionTokensDetails{
			ReasoningTokens: int64(usage.OutputTokensDetails.ReasoningTokens),
		}
	}
	return result
}

func convertLegacyResponsesUsageToCompat(usage *ResponsesUsage) *protocolcompat.ResponsesUsage {
	if usage == nil {
		return nil
	}

	result := &protocolcompat.ResponsesUsage{
		InputTokens:  int(usage.InputTokens),
		OutputTokens: int(usage.OutputTokens),
		TotalTokens:  int(usage.TotalTokens),
	}
	if usage.InputTokenDetails.CachedTokens > 0 {
		result.InputTokensDetails = &protocolcompat.ResponsesInputTokensDetails{
			CachedTokens: int(usage.InputTokenDetails.CachedTokens),
		}
	}
	if usage.OutputTokenDetails.ReasoningTokens > 0 {
		result.OutputTokensDetails = &protocolcompat.ResponsesOutputTokensDetails{
			ReasoningTokens: int(usage.OutputTokenDetails.ReasoningTokens),
		}
	}
	return result
}

func convertLegacyResponsesItemToCompat(item ResponsesItem) protocolcompat.ResponsesOutput {
	compatItem := protocolcompat.ResponsesOutput{
		Type:      item.Type,
		ID:        item.ID,
		Role:      item.Role,
		CallID:    item.CallID,
		Name:      item.Name,
		Arguments: item.Arguments,
	}
	if item.Status != nil {
		compatItem.Status = *item.Status
	}
	if item.Result != nil {
		compatItem.EncryptedContent = *item.Result
	}
	for _, summary := range item.Summary {
		compatItem.Summary = append(compatItem.Summary, protocolcompat.ResponsesSummary{
			Type: summary.Type,
			Text: summary.Text,
		})
	}
	if item.Content != nil {
		for _, contentItem := range item.Content.Items {
			compatItem.Content = append(compatItem.Content, protocolcompat.ResponsesContentPart{
				Type:     contentItem.Type,
				Text:     lo.FromPtr(contentItem.Text),
				ImageURL: lo.FromPtr(contentItem.ImageURL),
			})
		}
	}
	return compatItem
}

func buildToolCallsFromResponsesItems(items []ResponsesItem) []model.ToolCall {
	toolCalls := make([]model.ToolCall, 0)
	for idx, item := range items {
		if item.Type != "function_call" {
			continue
		}
		toolCalls = append(toolCalls, model.ToolCall{
			Index: idx,
			ID:    item.CallID,
			Type:  "function",
			Function: model.FunctionCall{
				Name:      item.Name,
				Arguments: item.Arguments,
			},
		})
	}
	return toolCalls
}

// ResponsesRequest represents the OpenAI Responses API request format.
type ResponsesRequest struct {
	Model             string                `json:"model"`
	Instructions      string                `json:"instructions,omitempty"`
	Input             ResponsesInput        `json:"input"`
	Tools             []ResponsesTool       `json:"tools,omitempty"`
	ToolChoice        *ResponsesToolChoice  `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool                 `json:"parallel_tool_calls,omitempty"`
	Stream            *bool                 `json:"stream,omitempty"`
	Text              *ResponsesTextOptions `json:"text,omitempty"`
	Store             *bool                 `json:"store,omitempty"`
	ServiceTier       *string               `json:"service_tier,omitempty"`
	User              *string               `json:"user,omitempty"`
	Metadata          map[string]string     `json:"metadata,omitempty"`
	MaxOutputTokens   *int64                `json:"max_output_tokens,omitempty"`
	Temperature       *float64              `json:"temperature,omitempty"`
	TopP              *float64              `json:"top_p,omitempty"`
	Reasoning         *ResponsesReasoning   `json:"reasoning,omitempty"`
}

type ResponsesInput struct {
	Text  *string
	Items []ResponsesItem
}

func (i ResponsesInput) MarshalJSON() ([]byte, error) {
	if i.Text != nil {
		return json.Marshal(i.Text)
	}
	return json.Marshal(i.Items)
}

func (i *ResponsesInput) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		i.Text = &text
		return nil
	}
	var items []ResponsesItem
	if err := json.Unmarshal(data, &items); err == nil {
		i.Items = items
		return nil
	}
	return fmt.Errorf("invalid input format")
}

type ResponsesItem struct {
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Role     string          `json:"role,omitempty"`
	Content  *ResponsesInput `json:"content,omitempty"`
	Status   *string         `json:"status,omitempty"`
	Text     *string         `json:"text,omitempty"`
	ImageURL *string         `json:"image_url,omitempty"`
	Detail   *string         `json:"detail,omitempty"`

	// Annotations for output_text content
	Annotations []ResponsesAnnotation `json:"annotations,omitempty"`

	// Function call fields
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// Function call output
	Output *ResponsesInput `json:"output,omitempty"`

	// Image generation fields
	Result       *string `json:"result,omitempty"`
	Background   *string `json:"background,omitempty"`
	OutputFormat *string `json:"output_format,omitempty"`
	Quality      *string `json:"quality,omitempty"`
	Size         *string `json:"size,omitempty"`

	// Reasoning fields
	Summary []ResponsesReasoningSummary `json:"summary,omitempty"`
}

type ResponsesReasoningSummary struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResponsesAnnotation struct {
	Type       string  `json:"type"`
	StartIndex *int    `json:"start_index,omitempty"`
	EndIndex   *int    `json:"end_index,omitempty"`
	URL        *string `json:"url,omitempty"`
	Title      *string `json:"title,omitempty"`
	FileID     *string `json:"file_id,omitempty"`
	Filename   *string `json:"filename,omitempty"`
}

type ResponsesTool struct {
	Type              string         `json:"type,omitempty"`
	Name              string         `json:"name,omitempty"`
	Description       string         `json:"description,omitempty"`
	Parameters        map[string]any `json:"parameters,omitempty"`
	Strict            *bool          `json:"strict,omitempty"`
	Background        string         `json:"background,omitempty"`
	OutputFormat      string         `json:"output_format,omitempty"`
	Quality           string         `json:"quality,omitempty"`
	Size              string         `json:"size,omitempty"`
	OutputCompression *int64         `json:"output_compression,omitempty"`
}

type ResponsesToolChoice struct {
	Mode *string `json:"mode,omitempty"`
	Type *string `json:"type,omitempty"`
	Name *string `json:"name,omitempty"`
}

func (t ResponsesToolChoice) MarshalJSON() ([]byte, error) {
	// If only Mode is set and it's a simple mode like "auto", "none", "required"
	if t.Mode != nil && t.Type == nil && t.Name == nil {
		return json.Marshal(*t.Mode)
	}
	// Otherwise, serialize as an object
	type Alias ResponsesToolChoice
	return json.Marshal(Alias(t))
}

type ResponsesTextOptions struct {
	Format    *ResponsesTextFormat `json:"format,omitempty"`
	Verbosity *string              `json:"verbosity,omitempty"`
}

type ResponsesTextFormat struct {
	Type   string          `json:"type,omitempty"`
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

type ResponsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

// ResponsesResponse represents the OpenAI Responses API response format.
type ResponsesResponse struct {
	Object    string          `json:"object"`
	ID        string          `json:"id"`
	Model     string          `json:"model"`
	CreatedAt int64           `json:"created_at"`
	Output    []ResponsesItem `json:"output"`
	Status    *string         `json:"status,omitempty"`
	Usage     *ResponsesUsage `json:"usage,omitempty"`
	Error     *ResponsesError `json:"error,omitempty"`
}

type ResponsesUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	InputTokenDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokens       int64 `json:"output_tokens"`
	OutputTokenDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	TotalTokens int64 `json:"total_tokens"`
}

type ResponsesError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ResponsesStreamEvent struct {
	Type           string             `json:"type"`
	SequenceNumber int                `json:"sequence_number"`
	Response       *ResponsesResponse `json:"response,omitempty"`
	OutputIndex    int                `json:"output_index"`
	Item           *ResponsesItem     `json:"item,omitempty"`
	ItemID         *string            `json:"item_id,omitempty"`
	ContentIndex   *int               `json:"content_index,omitempty"`
	Delta          string             `json:"delta,omitempty"`
	Text           string             `json:"text,omitempty"`
	Name           string             `json:"name,omitempty"`
	CallID         string             `json:"call_id,omitempty"`
	Arguments      string             `json:"arguments,omitempty"`
	SummaryIndex   *int               `json:"summary_index,omitempty"`
	Code           string             `json:"code,omitempty"`
	Message        string             `json:"message,omitempty"`
}

// Conversion functions

func ConvertToResponsesRequest(req *model.InternalLLMRequest) *ResponsesRequest {
	user := req.User
	metadata := req.Metadata
	// [fork] Anthropic metadata.user_id should behave like OpenAI user on Responses,
	// and must not be forwarded as Responses metadata for Codex-compatible upstreams.
	if req.RawAPIFormat == model.APIFormatAnthropicMessage {
		user, metadata = normalizeAnthropicMetadataForResponses(user, metadata)
	}

	result := &ResponsesRequest{
		Model:             req.Model,
		Temperature:       req.Temperature,
		TopP:              req.TopP,
		Stream:            req.Stream,
		Store:             req.Store,
		ServiceTier:       req.ServiceTier,
		User:              user,
		Metadata:          metadata,
		MaxOutputTokens:   req.MaxCompletionTokens,
		ParallelToolCalls: req.ParallelToolCalls,
	}

	// Convert instructions from system messages
	result.Instructions = convertInstructionsFromMessages(req.Messages)

	// Convert input from messages
	result.Input = convertInputFromMessages(req.Messages, req.TransformOptions)

	// Convert tools
	if len(req.Tools) > 0 {
		result.Tools = convertToolsToResponses(req.Tools)
	}

	// Convert tool choice
	if req.ToolChoice != nil {
		result.ToolChoice = convertToolChoiceToResponses(req.ToolChoice)
	}

	// Convert text options
	if req.ResponseFormat != nil {
		result.Text = &ResponsesTextOptions{
			Format: &ResponsesTextFormat{
				Type: req.ResponseFormat.Type,
			},
		}
	}

	// Convert reasoning
	if req.ReasoningEffort != "" || req.ReasoningBudget != nil {
		result.Reasoning = &ResponsesReasoning{
			Effort: req.ReasoningEffort,
		}
	}

	return result
}

func normalizeAnthropicMetadataForResponses(user *string, metadata map[string]string) (*string, map[string]string) {
	if len(metadata) == 0 {
		return user, metadata
	}

	userID := strings.TrimSpace(metadata["user_id"])
	if user == nil && userID != "" {
		user = lo.ToPtr(userID)
	}
	if userID == "" {
		return user, metadata
	}

	filtered := make(map[string]string, len(metadata)-1)
	for key, value := range metadata {
		if key == "user_id" {
			continue
		}
		filtered[key] = value
	}
	if len(filtered) == 0 {
		return user, nil
	}

	return user, filtered
}

func convertInstructionsFromMessages(msgs []model.Message) string {
	var instructions []string
	for _, msg := range msgs {
		if msg.Role != "system" && msg.Role != "developer" {
			continue
		}
		if msg.Content.Content != nil {
			instructions = append(instructions, *msg.Content.Content)
		}
		if len(msg.Content.MultipleContent) > 0 {
			var sb strings.Builder
			for _, p := range msg.Content.MultipleContent {
				if p.Type == "text" && p.Text != nil {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(*p.Text)
				}
			}
			if sb.Len() > 0 {
				instructions = append(instructions, sb.String())
			}
		}
	}
	return strings.Join(instructions, "\n")
}

func convertInputFromMessages(msgs []model.Message, transformOptions model.TransformOptions) ResponsesInput {
	if len(msgs) == 0 {
		return ResponsesInput{}
	}

	wasArrayFormat := transformOptions.ArrayInputs != nil && *transformOptions.ArrayInputs

	// Check for simple single user message
	nonSystemMsgs := make([]model.Message, 0)
	for _, msg := range msgs {
		if msg.Role != "system" && msg.Role != "developer" {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	if !wasArrayFormat && len(nonSystemMsgs) == 1 && nonSystemMsgs[0].Content.Content != nil && nonSystemMsgs[0].Role == "user" {
		return ResponsesInput{Text: nonSystemMsgs[0].Content.Content}
	}

	var items []ResponsesItem
	for _, msg := range msgs {
		switch msg.Role {
		case "system", "developer":
			continue
		case "user":
			items = append(items, convertUserMessageToResponses(msg))
		case "assistant":
			items = append(items, convertAssistantMessageToResponses(msg)...)
		case "tool":
			items = append(items, convertToolMessageToResponses(msg))
		}
	}

	return ResponsesInput{Items: items}
}

func convertUserMessageToResponses(msg model.Message) ResponsesItem {
	var contentItems []ResponsesItem

	if msg.Content.Content != nil {
		contentItems = append(contentItems, ResponsesItem{
			Type: "input_text",
			Text: msg.Content.Content,
		})
	} else {
		for _, p := range msg.Content.MultipleContent {
			switch p.Type {
			case "text":
				if p.Text != nil {
					contentItems = append(contentItems, ResponsesItem{
						Type: "input_text",
						Text: p.Text,
					})
				}
			case "image_url":
				if p.ImageURL != nil {
					contentItems = append(contentItems, ResponsesItem{
						Type:     "input_image",
						ImageURL: &p.ImageURL.URL,
						Detail:   p.ImageURL.Detail,
					})
				}
			}
		}
	}

	return ResponsesItem{
		Role:    msg.Role,
		Content: &ResponsesInput{Items: contentItems},
	}
}

func convertAssistantMessageToResponses(msg model.Message) []ResponsesItem {
	var items []ResponsesItem

	// Handle tool calls
	for _, tc := range msg.ToolCalls {
		items = append(items, ResponsesItem{
			Type:      "function_call",
			CallID:    tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	// Handle content
	var contentItems []ResponsesItem
	if msg.Content.Content != nil {
		contentItems = append(contentItems, ResponsesItem{
			Type: "output_text",
			Text: msg.Content.Content,
		})
	} else {
		for _, p := range msg.Content.MultipleContent {
			if p.Type == "text" && p.Text != nil {
				contentItems = append(contentItems, ResponsesItem{
					Type: "output_text",
					Text: p.Text,
				})
			}
		}
	}

	if len(contentItems) > 0 {
		items = append(items, ResponsesItem{
			Type:    "message",
			Role:    msg.Role,
			Status:  lo.ToPtr("completed"),
			Content: &ResponsesInput{Items: contentItems},
		})
	}

	return items
}

func convertToolMessageToResponses(msg model.Message) ResponsesItem {
	var output ResponsesInput

	if msg.Content.Content != nil {
		output.Text = msg.Content.Content
	} else if len(msg.Content.MultipleContent) > 0 {
		for _, p := range msg.Content.MultipleContent {
			if p.Type == "text" && p.Text != nil {
				output.Items = append(output.Items, ResponsesItem{
					Type: "input_text",
					Text: p.Text,
				})
			}
		}
	}

	if output.Text == nil && len(output.Items) == 0 {
		output.Text = lo.ToPtr("")
	}

	return ResponsesItem{
		Type:   "function_call_output",
		CallID: lo.FromPtr(msg.ToolCallID),
		Output: &output,
	}
}

func convertToolsToResponses(tools []model.Tool) []ResponsesTool {
	result := make([]ResponsesTool, 0, len(tools))
	for _, tool := range tools {
		switch tool.Type {
		case "function":
			rt := ResponsesTool{
				Type:        "function",
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Strict:      tool.Function.Strict,
			}
			if len(tool.Function.Parameters) > 0 {
				var params map[string]any
				if err := json.Unmarshal(tool.Function.Parameters, &params); err == nil {
					rt.Parameters = params
				}
			}
			result = append(result, rt)
		case "image_generation":
			rt := ResponsesTool{
				Type: "image_generation",
			}
			if tool.ImageGeneration != nil {
				rt.Background = tool.ImageGeneration.Background
				rt.OutputFormat = tool.ImageGeneration.OutputFormat
				rt.Quality = tool.ImageGeneration.Quality
				rt.Size = tool.ImageGeneration.Size
				rt.OutputCompression = tool.ImageGeneration.OutputCompression
			}
			result = append(result, rt)
		}
	}
	return result
}

func convertToolChoiceToResponses(tc *model.ToolChoice) *ResponsesToolChoice {
	if tc == nil {
		return nil
	}

	result := &ResponsesToolChoice{}
	if tc.ToolChoice != nil {
		result.Mode = tc.ToolChoice
	} else if tc.NamedToolChoice != nil {
		result.Type = &tc.NamedToolChoice.Type
		result.Name = &tc.NamedToolChoice.Function.Name
	}
	return result
}

func convertToLLMResponseFromResponses(resp *ResponsesResponse) *model.InternalLLMResponse {
	if resp == nil {
		return &model.InternalLLMResponse{
			Object: "chat.completion",
		}
	}

	result := &model.InternalLLMResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Model:   resp.Model,
		Created: resp.CreatedAt,
	}

	var (
		contentParts     []model.MessageContentPart
		textContent      strings.Builder
		reasoningContent strings.Builder
		toolCalls        []model.ToolCall
	)

	for _, outputItem := range resp.Output {
		switch outputItem.Type {
		case "message":
			if outputItem.Content != nil {
				for _, item := range outputItem.Content.Items {
					if item.Type == "output_text" && item.Text != nil {
						textContent.WriteString(*item.Text)
					}
				}
			}
		case "output_text":
			if outputItem.Text != nil {
				textContent.WriteString(*outputItem.Text)
			}
		case "function_call":
			toolCalls = append(toolCalls, model.ToolCall{
				ID:   outputItem.CallID,
				Type: "function",
				Function: model.FunctionCall{
					Name:      outputItem.Name,
					Arguments: outputItem.Arguments,
				},
			})
		case "reasoning":
			for _, summary := range outputItem.Summary {
				reasoningContent.WriteString(summary.Text)
			}
		case "image_generation_call":
			if outputItem.Result != nil && *outputItem.Result != "" {
				outputFormat := "png"
				if outputItem.OutputFormat != nil {
					outputFormat = *outputItem.OutputFormat
				}
				contentParts = append(contentParts, model.MessageContentPart{
					Type: "image_url",
					ImageURL: &model.ImageURL{
						URL: "data:image/" + outputFormat + ";base64," + *outputItem.Result,
					},
				})
			}
		}
	}

	choice := model.Choice{
		Index: 0,
		Message: &model.Message{
			Role:      "assistant",
			ToolCalls: toolCalls,
		},
	}

	// Set reasoning content if present
	if reasoningContent.Len() > 0 {
		choice.Message.ReasoningContent = lo.ToPtr(reasoningContent.String())
	}

	// Set message content
	if textContent.Len() > 0 {
		if len(contentParts) > 0 {
			textPart := model.MessageContentPart{
				Type: "text",
				Text: lo.ToPtr(textContent.String()),
			}
			contentParts = append([]model.MessageContentPart{textPart}, contentParts...)
			choice.Message.Content = model.MessageContent{
				MultipleContent: contentParts,
			}
		} else {
			choice.Message.Content = model.MessageContent{
				Content: lo.ToPtr(textContent.String()),
			}
		}
	} else if len(contentParts) > 0 {
		choice.Message.Content = model.MessageContent{
			MultipleContent: contentParts,
		}
	}

	// Set finish reason based on status
	if len(toolCalls) > 0 {
		choice.FinishReason = lo.ToPtr("tool_calls")
	} else if resp.Status != nil {
		switch *resp.Status {
		case "completed":
			choice.FinishReason = lo.ToPtr("stop")
		case "failed":
			choice.FinishReason = lo.ToPtr("error")
		case "incomplete":
			choice.FinishReason = lo.ToPtr("length")
		}
	}

	result.Choices = []model.Choice{choice}
	result.Usage = convertResponsesUsage(resp.Usage)

	return result
}

func convertResponsesUsage(usage *ResponsesUsage) *model.Usage {
	if usage == nil {
		return nil
	}

	result := &model.Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.TotalTokens,
	}

	if usage.InputTokenDetails.CachedTokens > 0 {
		result.PromptTokensDetails = &model.PromptTokensDetails{
			CachedTokens: usage.InputTokenDetails.CachedTokens,
		}
	}

	if usage.OutputTokenDetails.ReasoningTokens > 0 {
		result.CompletionTokensDetails = &model.CompletionTokensDetails{
			ReasoningTokens: usage.OutputTokenDetails.ReasoningTokens,
		}
	}

	return result
}
