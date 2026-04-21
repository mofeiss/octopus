package authropic

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

	anthropicModel "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/protocolcompat"
	"github.com/bestruirui/octopus/internal/utils/xurl"
)

type MessageOutbound struct {
	// Stream state tracking
	streamID    string
	streamModel string
	streamUsage *model.Usage
	toolIndex   int
	toolCalls   map[int]*model.ToolCall
	initialized bool

	// [fork] Reuse the sub2api Anthropic -> Responses -> Chat stream pipeline to
	// keep cross-protocol tool-call parsing aligned with the upstream gateway.
	anthropicResponsesState *protocolcompat.AnthropicEventToResponsesState
	responsesChatState      *protocolcompat.ResponsesEventToChatState
}

func (o *MessageOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}

	requestBody := any(convertToAnthropicRequest(request))
	if compatReq, ok, err := buildCompatAnthropicRequest(request); err != nil {
		return nil, err
	} else if ok {
		requestBody = compatReq
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	// For streaming requests, Anthropic returns Server-Sent Events.
	if request.Stream != nil && *request.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("X-API-Key", key)

	// Parse and set URL
	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}

	parsedUrl.Path = parsedUrl.Path + "/messages"
	// Pass through the original query parameters exactly as-is
	if request.Query != nil {
		parsedUrl.RawQuery = request.Query.Encode()
	}
	req.URL = parsedUrl

	return req, nil
}

func (o *MessageOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
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
		var errResp anthropicModel.AnthropicError
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, &model.ResponseError{
				StatusCode: response.StatusCode,
				Detail: model.ErrorDetail{
					Message: errResp.Error.Message,
					Type:    errResp.Error.Type,
				},
			}
		}
		return nil, fmt.Errorf("HTTP error %d: %s", response.StatusCode, string(body))
	}

	var anthropicResp anthropicModel.Message
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		var compatResp protocolcompat.AnthropicResponse
		if compatErr := json.Unmarshal(body, &compatResp); compatErr != nil {
			return nil, fmt.Errorf("failed to unmarshal anthropic response: %w", err)
		}

		responsesResp := protocolcompat.AnthropicToResponsesResponse(&compatResp)
		chatResp := protocolcompat.ResponsesToChatCompletions(responsesResp, compatResp.Model)
		internalResp, bridgeErr := protocolcompat.ChatResponseToInternal(chatResp)
		if bridgeErr != nil {
			return nil, fmt.Errorf("failed to bridge compat anthropic response: %w", bridgeErr)
		}
		internalResp.Usage = convertCompatAnthropicUsage(&compatResp.Usage)
		return internalResp, nil
	}

	// [fork] Prefer the sub2api-compatible bridge first, while keeping the legacy
	// converter as fallback for provider-specific JSON differences.
	compatResp := convertLegacyAnthropicResponseToCompat(&anthropicResp)
	responsesResp := protocolcompat.AnthropicToResponsesResponse(compatResp)
	chatResp := protocolcompat.ResponsesToChatCompletions(responsesResp, compatResp.Model)
	internalResp, bridgeErr := protocolcompat.ChatResponseToInternal(chatResp)
	if bridgeErr == nil {
		internalResp.Usage = convertCompatAnthropicUsage(&compatResp.Usage)
		return internalResp, nil
	}

	// Convert to internal response
	return convertToLLMResponse(&anthropicResp), nil
}

func (o *MessageOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	if len(eventData) == 0 {
		return nil, nil
	}

	// Handle [DONE] marker
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return &model.InternalLLMResponse{
			Object: "[DONE]",
		}, nil
	}

	// Initialize state if needed
	if !o.initialized {
		o.toolCalls = make(map[int]*model.ToolCall)
		o.toolIndex = -1
		o.initialized = true
	}
	if o.anthropicResponsesState == nil {
		o.anthropicResponsesState = protocolcompat.NewAnthropicEventToResponsesState()
	}
	if o.responsesChatState == nil {
		o.responsesChatState = protocolcompat.NewResponsesEventToChatState()
		o.responsesChatState.IncludeUsage = true
	}

	// [fork] Parse Anthropic SSE with the sub2api-compatible state machine first.
	var streamEvent protocolcompat.AnthropicStreamEvent
	if err := json.Unmarshal(eventData, &streamEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream event: %w", err)
	}

	o.trackCompatAnthropicUsage(&streamEvent)

	responsesEvents := protocolcompat.AnthropicEventToResponsesEvents(&streamEvent, o.anthropicResponsesState)
	compatChunks := make([]protocolcompat.ChatCompletionsChunk, 0, len(responsesEvents))
	for i := range responsesEvents {
		compatChunks = append(compatChunks, protocolcompat.ResponsesEventToChatChunks(&responsesEvents[i], o.responsesChatState)...)
	}

	resp := mergeCompatAnthropicChatChunks(compatChunks)
	if resp == nil {
		return nil, nil
	}

	internalResp, err := protocolcompat.ChatChunkToInternal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to bridge compat anthropic stream chunk: %w", err)
	}

	if streamEvent.Type == "message_start" && o.streamUsage != nil {
		internalResp.Usage = o.streamUsage
	}
	if streamEvent.Type == "message_stop" && o.streamUsage != nil {
		internalResp.Usage = o.streamUsage
	}

	return internalResp, nil
}

// convertToAnthropicRequest converts internal LLM request to Anthropic format
func convertToAnthropicRequest(req *model.InternalLLMRequest) *anthropicModel.MessageRequest {
	result := &anthropicModel.MessageRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		MaxTokens:   resolveMaxTokens(req),
		System:      convertSystemPrompt(req),
	}

	if req.Metadata != nil && req.Metadata["user_id"] != "" {
		result.Metadata = &anthropicModel.AnthropicMetadata{UserID: req.Metadata["user_id"]}
	}

	// Convert messages
	result.Messages = convertMessages(req)

	// Convert tools
	if len(req.Tools) > 0 {
		result.Tools = convertTools(req.Tools)
	}

	// Convert stop sequences
	if req.Stop != nil {
		result.StopSequences = convertStopSequences(req.Stop)
	}

	// Convert thinking/reasoning
	if req.ReasoningEffort != "" {
		if req.AdaptiveThinking {
			result.Thinking = &anthropicModel.Thinking{
				Type: anthropicModel.ThinkingTypeAdaptive,
			}
			result.OutputConfig = &anthropicModel.OutputConfig{
				Effort: req.ReasoningEffort,
			}
		} else {
			result.Thinking = &anthropicModel.Thinking{
				Type:         anthropicModel.ThinkingTypeEnabled,
				BudgetTokens: getThinkingBudget(req.ReasoningEffort, req.ReasoningBudget),
			}
		}
	}

	return result
}

func resolveMaxTokens(req *model.InternalLLMRequest) int64 {
	var maxtoken int64 = 1
	switch {
	case req.MaxTokens != nil:
		maxtoken = *req.MaxTokens
	case req.MaxCompletionTokens != nil:
		maxtoken = *req.MaxCompletionTokens
	default:
		maxtoken = 8192
	}
	if maxtoken < 1 {
		maxtoken = 1
	}
	return maxtoken
}

func convertSystemPrompt(req *model.InternalLLMRequest) *anthropicModel.SystemPrompt {
	var systemMessages []model.Message
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemMessages = append(systemMessages, msg)
		}
	}

	if len(systemMessages) == 0 {
		return nil
	}

	if len(systemMessages) == 1 {
		return &anthropicModel.SystemPrompt{
			MultiplePrompts: []anthropicModel.SystemPromptPart{{
				Type:         "text",
				Text:         lo.FromPtr(systemMessages[0].Content.Content),
				CacheControl: convertCacheControl(systemMessages[0].CacheControl),
			}},
		}
	}

	parts := make([]anthropicModel.SystemPromptPart, 0, len(systemMessages))
	for _, msg := range systemMessages {
		parts = append(parts, anthropicModel.SystemPromptPart{
			Type:         "text",
			Text:         lo.FromPtr(msg.Content.Content),
			CacheControl: convertCacheControl(msg.CacheControl),
		})
	}
	return &anthropicModel.SystemPrompt{
		MultiplePrompts: parts,
	}
}

func convertMessages(req *model.InternalLLMRequest) []anthropicModel.MessageParam {
	messages := make([]anthropicModel.MessageParam, 0, len(req.Messages))
	processedIndexes := make(map[int]bool)

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			continue
		}

		converted := convertSingleMessage(msg, req.Messages, processedIndexes)
		messages = append(messages, converted...)
	}

	return messages
}

func convertSingleMessage(msg model.Message, allMessages []model.Message, processedIndexes map[int]bool) []anthropicModel.MessageParam {
	switch msg.Role {
	case "tool":
		return convertToolMessage(msg, allMessages, processedIndexes)
	case "user":
		if msg.MessageIndex != nil && processedIndexes[*msg.MessageIndex] {
			return nil
		}
		return convertUserMessage(msg)
	case "assistant":
		return convertAssistantMessage(msg)
	default:
		return nil
	}
}

func convertToolMessage(msg model.Message, allMessages []model.Message, processedIndexes map[int]bool) []anthropicModel.MessageParam {
	if msg.MessageIndex == nil {
		return []anthropicModel.MessageParam{{
			Role: "user",
			Content: anthropicModel.MessageContent{
				MultipleContent: []anthropicModel.MessageContentBlock{convertToolResultBlock(msg)},
			},
		}}
	}

	if processedIndexes[*msg.MessageIndex] {
		return nil
	}

	var toolMsgs []model.Message
	for _, m := range allMessages {
		if m.Role == "tool" && m.MessageIndex != nil && *m.MessageIndex == *msg.MessageIndex {
			toolMsgs = append(toolMsgs, m)
		}
	}

	if len(toolMsgs) == 0 {
		return nil
	}

	contentBlocks := make([]anthropicModel.MessageContentBlock, 0, len(toolMsgs))
	for _, tm := range toolMsgs {
		contentBlocks = append(contentBlocks, convertToolResultBlock(tm))
	}

	// Merge the associated user message content (if any) into the same Anthropic user message.
	// In Anthropic Messages, tool_result blocks live inside a user message's content array.
	// Our internal format represents tool results as separate "tool" role messages, but the
	// original Anthropic request may also include additional user content alongside tool_result.
	if userMsg := findUserMessageByIndex(allMessages, *msg.MessageIndex); userMsg != nil {
		userContent := buildMessageContent(*userMsg)
		if len(userContent.MultipleContent) > 0 {
			contentBlocks = append(contentBlocks, userContent.MultipleContent...)
		} else if userContent.Content != nil && *userContent.Content != "" {
			contentBlocks = append(contentBlocks, anthropicModel.MessageContentBlock{
				Type: "text",
				Text: userContent.Content,
			})
		}
	}

	processedIndexes[*msg.MessageIndex] = true

	return []anthropicModel.MessageParam{{
		Role:    "user",
		Content: anthropicModel.MessageContent{MultipleContent: contentBlocks},
	}}
}

func findUserMessageByIndex(allMessages []model.Message, messageIndex int) *model.Message {
	for i := range allMessages {
		m := &allMessages[i]
		if m.Role == "user" && m.MessageIndex != nil && *m.MessageIndex == messageIndex {
			return m
		}
	}
	return nil
}

func convertToolResultBlock(msg model.Message) anthropicModel.MessageContentBlock {
	block := anthropicModel.MessageContentBlock{
		Type:         "tool_result",
		ToolUseID:    msg.ToolCallID,
		CacheControl: convertCacheControl(msg.CacheControl),
		IsError:      msg.ToolCallIsError,
	}

	if msg.Content.Content != nil {
		block.Content = &anthropicModel.MessageContent{
			Content: msg.Content.Content,
		}
	} else if len(msg.Content.MultipleContent) > 0 {
		blocks := make([]anthropicModel.MessageContentBlock, 0, len(msg.Content.MultipleContent))
		for _, part := range msg.Content.MultipleContent {
			if part.Type == "text" && part.Text != nil {
				blocks = append(blocks, anthropicModel.MessageContentBlock{
					Type: "text",
					Text: part.Text,
				})
			}
		}
		block.Content = &anthropicModel.MessageContent{
			MultipleContent: blocks,
		}
	}

	return block
}

func convertUserMessage(msg model.Message) []anthropicModel.MessageParam {
	content := buildMessageContent(msg)
	return []anthropicModel.MessageParam{{Role: "user", Content: content}}
}

func convertAssistantMessage(msg model.Message) []anthropicModel.MessageParam {
	if len(msg.ToolCalls) > 0 {
		return convertAssistantWithToolCalls(msg)
	}

	content := buildMessageContent(msg)
	return []anthropicModel.MessageParam{{Role: "assistant", Content: content}}
}

func convertAssistantWithToolCalls(msg model.Message) []anthropicModel.MessageParam {
	var blocks []anthropicModel.MessageContentBlock

	// Add thinking block if present
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		blocks = append(blocks, anthropicModel.MessageContentBlock{
			Type:      "thinking",
			Thinking:  msg.ReasoningContent,
			Signature: msg.ReasoningSignature,
		})
	}

	// Add text content if present
	if msg.Content.Content != nil && *msg.Content.Content != "" {
		blocks = append(blocks, anthropicModel.MessageContentBlock{
			Type:         "text",
			Text:         msg.Content.Content,
			CacheControl: convertCacheControl(msg.CacheControl),
		})
	} else if len(msg.Content.MultipleContent) > 0 {
		for _, part := range msg.Content.MultipleContent {
			if part.Type == "text" && part.Text != nil {
				blocks = append(blocks, anthropicModel.MessageContentBlock{
					Type:         "text",
					Text:         part.Text,
					CacheControl: convertCacheControl(part.CacheControl),
				})
			}
		}
	}

	// Add tool calls
	for _, toolCall := range msg.ToolCalls {
		input := json.RawMessage("{}")
		if toolCall.Function.Arguments != "" {
			if json.Valid([]byte(toolCall.Function.Arguments)) {
				input = json.RawMessage(toolCall.Function.Arguments)
			}
		}
		blocks = append(blocks, anthropicModel.MessageContentBlock{
			Type:         "tool_use",
			ID:           toolCall.ID,
			Name:         &toolCall.Function.Name,
			Input:        input,
			CacheControl: convertCacheControl(toolCall.CacheControl),
		})
	}

	if len(blocks) == 0 {
		return nil
	}

	return []anthropicModel.MessageParam{{
		Role:    "assistant",
		Content: anthropicModel.MessageContent{MultipleContent: blocks},
	}}
}

func buildMessageContent(msg model.Message) anthropicModel.MessageContent {
	// Handle simple string content
	if msg.Content.Content != nil {
		if msg.CacheControl != nil || hasThinkingContent(msg) {
			return buildMultipleContentWithThinking(msg)
		}
		return anthropicModel.MessageContent{Content: msg.Content.Content}
	}

	// Handle multiple content parts
	if len(msg.Content.MultipleContent) > 0 {
		return convertMultiplePartContent(msg)
	}

	return anthropicModel.MessageContent{}
}

func hasThinkingContent(msg model.Message) bool {
	return msg.ReasoningContent != nil && *msg.ReasoningContent != ""
}

func buildMultipleContentWithThinking(msg model.Message) anthropicModel.MessageContent {
	var blocks []anthropicModel.MessageContentBlock

	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		blocks = append(blocks, anthropicModel.MessageContentBlock{
			Type:      "thinking",
			Thinking:  msg.ReasoningContent,
			Signature: msg.ReasoningSignature,
		})
	}

	blocks = append(blocks, anthropicModel.MessageContentBlock{
		Type:         "text",
		Text:         msg.Content.Content,
		CacheControl: convertCacheControl(msg.CacheControl),
	})

	return anthropicModel.MessageContent{MultipleContent: blocks}
}

func convertMultiplePartContent(msg model.Message) anthropicModel.MessageContent {
	blocks := make([]anthropicModel.MessageContentBlock, 0, len(msg.Content.MultipleContent))

	for _, part := range msg.Content.MultipleContent {
		switch part.Type {
		case "text":
			if part.Text != nil {
				blocks = append(blocks, anthropicModel.MessageContentBlock{
					Type:         "text",
					Text:         part.Text,
					CacheControl: convertCacheControl(part.CacheControl),
				})
			}
		case "image_url":
			if part.ImageURL != nil && part.ImageURL.URL != "" {
				block := convertImageURLToBlock(part)
				if block != nil {
					blocks = append(blocks, *block)
				}
			}
		}
	}

	// Add tool calls if present
	for _, toolCall := range msg.ToolCalls {
		input := json.RawMessage("{}")
		if toolCall.Function.Arguments != "" {
			if json.Valid([]byte(toolCall.Function.Arguments)) {
				input = json.RawMessage(toolCall.Function.Arguments)
			}
		}
		blocks = append(blocks, anthropicModel.MessageContentBlock{
			Type:         "tool_use",
			ID:           toolCall.ID,
			Name:         &toolCall.Function.Name,
			Input:        input,
			CacheControl: convertCacheControl(toolCall.CacheControl),
		})
	}

	if len(blocks) == 0 {
		return anthropicModel.MessageContent{}
	}

	return anthropicModel.MessageContent{MultipleContent: blocks}
}

func convertImageURLToBlock(part model.MessageContentPart) *anthropicModel.MessageContentBlock {
	if part.ImageURL == nil || part.ImageURL.URL == "" {
		return nil
	}

	url := part.ImageURL.URL
	if parsed := xurl.ParseDataURL(url); parsed != nil {
		return &anthropicModel.MessageContentBlock{
			Type: "image",
			Source: &anthropicModel.ImageSource{
				Type:      "base64",
				MediaType: parsed.MediaType,
				Data:      parsed.Data,
			},
			CacheControl: convertCacheControl(part.CacheControl),
		}
	}

	return &anthropicModel.MessageContentBlock{
		Type: "image",
		Source: &anthropicModel.ImageSource{
			Type: "url",
			URL:  part.ImageURL.URL,
		},
		CacheControl: convertCacheControl(part.CacheControl),
	}
}

func convertTools(tools []model.Tool) []anthropicModel.Tool {
	result := make([]anthropicModel.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		result = append(result, anthropicModel.Tool{
			Name:         tool.Function.Name,
			Description:  tool.Function.Description,
			InputSchema:  tool.Function.Parameters,
			CacheControl: convertCacheControl(tool.CacheControl),
		})
	}
	return result
}

func convertStopSequences(stop *model.Stop) []string {
	if stop == nil {
		return nil
	}
	if stop.Stop != nil {
		return []string{*stop.Stop}
	}
	if len(stop.MultipleStop) > 0 {
		return stop.MultipleStop
	}
	return nil
}

func convertCacheControl(cc *model.CacheControl) *anthropicModel.CacheControl {
	if cc == nil {
		return nil
	}
	return &anthropicModel.CacheControl{
		Type: cc.Type,
		TTL:  cc.TTL,
	}
}

func getThinkingBudget(effort string, budget *int64) *int64 {
	if budget != nil {
		return budget
	}

	var result int64
	switch effort {
	case anthropicModel.EffortLow:
		result = 1024
	case anthropicModel.EffortMedium:
		result = 8192
	case anthropicModel.EffortHigh:
		result = 32768
	default:
		result = 8192
	}
	return &result
}

// Response conversion functions

func convertToLLMResponse(resp *anthropicModel.Message) *model.InternalLLMResponse {
	if resp == nil {
		return &model.InternalLLMResponse{
			Object: "chat.completion",
		}
	}

	result := &model.InternalLLMResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Model:   resp.Model,
		Created: 0,
	}

	var (
		content           model.MessageContent
		thinkingText      *string
		thinkingSignature *string
		toolCalls         []model.ToolCall
		textParts         []string
	)

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			if block.Text != nil && *block.Text != "" {
				textParts = append(textParts, *block.Text)
				content.MultipleContent = append(content.MultipleContent, model.MessageContentPart{
					Type: "text",
					Text: block.Text,
				})
			}
		case "tool_use":
			if block.ID != "" && block.Name != nil {
				input := "{}"
				if len(block.Input) > 0 {
					input = string(block.Input)
				}
				toolCalls = append(toolCalls, model.ToolCall{
					ID:   block.ID,
					Type: "function",
					Function: model.FunctionCall{
						Name:      *block.Name,
						Arguments: input,
					},
				})
			}
		case "thinking":
			if block.Thinking != nil {
				thinkingText = block.Thinking
			}
			thinkingSignature = block.Signature
		}
	}

	// If we only have text content, use simple string format
	if len(textParts) > 0 && len(content.MultipleContent) == len(textParts) {
		allText := strings.Join(textParts, "")
		content.Content = &allText
		content.MultipleContent = nil
	}

	message := &model.Message{
		Role:               resp.Role,
		Content:            content,
		ToolCalls:          toolCalls,
		ReasoningContent:   thinkingText,
		ReasoningSignature: thinkingSignature,
	}

	choice := model.Choice{
		Index:        0,
		Message:      message,
		FinishReason: convertStopReason(resp.StopReason),
	}

	result.Choices = []model.Choice{choice}
	result.Usage = convertAnthropicUsage(resp.Usage)

	return result
}

func convertStopReason(stopReason *string) *string {
	if stopReason == nil {
		return nil
	}

	switch *stopReason {
	case "end_turn":
		return lo.ToPtr("stop")
	case "max_tokens":
		return lo.ToPtr("length")
	case "stop_sequence", "pause_turn":
		return lo.ToPtr("stop")
	case "tool_use":
		return lo.ToPtr("tool_calls")
	case "refusal":
		return lo.ToPtr("content_filter")
	default:
		return stopReason
	}
}

func convertAnthropicUsage(usage *anthropicModel.Usage) *model.Usage {
	if usage == nil {
		return nil
	}

	result := &model.Usage{
		PromptTokens:             usage.InputTokens,
		CompletionTokens:         usage.OutputTokens,
		TotalTokens:              usage.InputTokens + usage.OutputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		AnthropicUsage:           true,
	}

	if usage.CacheReadInputTokens > 0 {
		result.PromptTokensDetails = &model.PromptTokensDetails{
			CachedTokens: usage.CacheReadInputTokens,
		}
	}
	return result
}

// [fork] Reuse the sub2api request conversion path when the inbound protocol is
// OpenAI Responses or OpenAI Chat and the selected upstream is Anthropic.
func buildCompatAnthropicRequest(request *model.InternalLLMRequest) (*protocolcompat.AnthropicRequest, bool, error) {
	if request == nil || len(request.RawRequest) == 0 {
		return nil, false, nil
	}

	switch request.RawAPIFormat {
	case model.APIFormatOpenAIResponse:
		var raw protocolcompat.ResponsesRequest
		if err := json.Unmarshal(request.RawRequest, &raw); err != nil {
			return nil, false, fmt.Errorf("failed to decode raw responses request: %w", err)
		}

		raw.Model = request.Model
		raw.Stream = request.Stream != nil && *request.Stream

		compatReq, err := protocolcompat.ResponsesToAnthropicRequest(&raw)
		if err != nil {
			return nil, false, fmt.Errorf("failed to convert raw responses request to anthropic request: %w", err)
		}
		compatReq.Model = request.Model
		compatReq.Stream = request.Stream != nil && *request.Stream
		return compatReq, true, nil
	case model.APIFormatOpenAIChatCompletion:
		var raw protocolcompat.ChatCompletionsRequest
		if err := json.Unmarshal(request.RawRequest, &raw); err != nil {
			return nil, false, fmt.Errorf("failed to decode raw chat completions request: %w", err)
		}

		responsesReq, err := protocolcompat.ChatCompletionsToResponses(&raw)
		if err != nil {
			return nil, false, fmt.Errorf("failed to convert raw chat completions request to responses request: %w", err)
		}
		responsesReq.Model = request.Model
		responsesReq.Stream = request.Stream != nil && *request.Stream

		compatReq, err := protocolcompat.ResponsesToAnthropicRequest(responsesReq)
		if err != nil {
			return nil, false, fmt.Errorf("failed to convert compat responses request to anthropic request: %w", err)
		}
		compatReq.Model = request.Model
		compatReq.Stream = request.Stream != nil && *request.Stream
		return compatReq, true, nil
	default:
		return nil, false, nil
	}
}

func convertCompatAnthropicUsage(usage *protocolcompat.AnthropicUsage) *model.Usage {
	if usage == nil {
		return nil
	}

	result := &model.Usage{
		PromptTokens:             int64(usage.InputTokens),
		CompletionTokens:         int64(usage.OutputTokens),
		TotalTokens:              int64(usage.InputTokens + usage.OutputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens),
		CacheCreationInputTokens: int64(usage.CacheCreationInputTokens),
		AnthropicUsage:           true,
	}
	if usage.CacheReadInputTokens > 0 {
		result.PromptTokensDetails = &model.PromptTokensDetails{
			CachedTokens: int64(usage.CacheReadInputTokens),
		}
	}
	return result
}

func (o *MessageOutbound) trackCompatAnthropicUsage(event *protocolcompat.AnthropicStreamEvent) {
	if event == nil {
		return
	}

	switch event.Type {
	case "message_start":
		if event.Message == nil {
			return
		}
		o.streamID = event.Message.ID
		o.streamModel = event.Message.Model
		if usage := convertCompatAnthropicUsage(&event.Message.Usage); usage != nil {
			o.streamUsage = usage
		}
	case "message_delta":
		if event.Usage == nil {
			return
		}
		usage := convertCompatAnthropicUsage(event.Usage)
		if o.streamUsage != nil {
			usage.PromptTokens = o.streamUsage.PromptTokens
			usage.CacheCreationInputTokens = o.streamUsage.CacheCreationInputTokens
			if usage.PromptTokensDetails == nil {
				usage.PromptTokensDetails = o.streamUsage.PromptTokensDetails
			}
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens + usage.CacheCreationInputTokens
			if usage.PromptTokensDetails != nil {
				usage.TotalTokens += usage.PromptTokensDetails.CachedTokens
			}
		}
		o.streamUsage = usage
	}
}

func mergeCompatAnthropicChatChunks(chunks []protocolcompat.ChatCompletionsChunk) *protocolcompat.ChatCompletionsChunk {
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

func convertLegacyAnthropicResponseToCompat(resp *anthropicModel.Message) *protocolcompat.AnthropicResponse {
	if resp == nil {
		return &protocolcompat.AnthropicResponse{}
	}

	compatResp := &protocolcompat.AnthropicResponse{
		ID:    resp.ID,
		Type:  resp.Type,
		Role:  resp.Role,
		Model: resp.Model,
		Usage: protocolcompat.AnthropicUsage{},
	}
	if resp.StopReason != nil {
		compatResp.StopReason = *resp.StopReason
	}
	if resp.StopSequence != nil {
		compatResp.StopSequence = resp.StopSequence
	}
	if resp.Usage != nil {
		compatResp.Usage = protocolcompat.AnthropicUsage{
			InputTokens:              int(resp.Usage.InputTokens),
			OutputTokens:             int(resp.Usage.OutputTokens),
			CacheCreationInputTokens: int(resp.Usage.CacheCreationInputTokens),
			CacheReadInputTokens:     int(resp.Usage.CacheReadInputTokens),
		}
	}

	for _, block := range resp.Content {
		compatBlock := protocolcompat.AnthropicContentBlock{
			Type:      block.Type,
			Text:      lo.FromPtr(block.Text),
			Thinking:  lo.FromPtr(block.Thinking),
			Signature: lo.FromPtr(block.Signature),
			ID:        block.ID,
			Name:      lo.FromPtr(block.Name),
		}
		if block.Source != nil {
			compatBlock.Source = &protocolcompat.AnthropicImageSource{
				Type:      block.Source.Type,
				MediaType: block.Source.MediaType,
				Data:      block.Source.Data,
			}
		}
		if len(block.Input) > 0 {
			compatBlock.Input = append(json.RawMessage(nil), block.Input...)
		}
		if block.ToolUseID != nil {
			compatBlock.ToolUseID = *block.ToolUseID
		}
		if block.Content != nil {
			contentJSON, err := json.Marshal(block.Content)
			if err == nil {
				compatBlock.Content = contentJSON
			}
		}
		if block.IsError != nil {
			compatBlock.IsError = *block.IsError
		}
		compatResp.Content = append(compatResp.Content, compatBlock)
	}

	return compatResp
}
