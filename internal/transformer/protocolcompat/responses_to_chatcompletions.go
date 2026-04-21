package protocolcompat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// [fork] Ported from sub2api to keep Responses -> Chat response conversion
// aligned with the upstream gateway behavior.
func ResponsesToChatCompletions(resp *ResponsesResponse, model string) *ChatCompletionsResponse {
	id := resp.ID
	if id == "" {
		id = generateChatCmplID()
	}

	out := &ChatCompletionsResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
	}

	var contentText string
	var reasoningText string
	var toolCalls []ChatToolCall

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					contentText += part.Text
				}
			}
		case "function_call":
			toolCalls = append(toolCalls, ChatToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: ChatFunctionCall{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		case "reasoning":
			for _, s := range item.Summary {
				if s.Type == "summary_text" && s.Text != "" {
					reasoningText += s.Text
				}
			}
		case "web_search_call":
		}
	}

	msg := ChatMessage{Role: "assistant"}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	if contentText != "" {
		raw, _ := json.Marshal(contentText)
		msg.Content = raw
	}
	if reasoningText != "" {
		msg.ReasoningContent = reasoningText
	}

	finishReason := responsesStatusToChatFinishReason(resp.Status, resp.IncompleteDetails, toolCalls)

	out.Choices = []ChatChoice{{
		Index:        0,
		Message:      msg,
		FinishReason: finishReason,
	}}

	if resp.Usage != nil {
		usage := &ChatUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
		if resp.Usage.InputTokensDetails != nil && resp.Usage.InputTokensDetails.CachedTokens > 0 {
			usage.PromptTokensDetails = &ChatTokenDetails{
				CachedTokens: resp.Usage.InputTokensDetails.CachedTokens,
			}
		}
		out.Usage = usage
	}

	return out
}

func responsesStatusToChatFinishReason(status string, details *ResponsesIncompleteDetails, toolCalls []ChatToolCall) string {
	switch status {
	case "incomplete":
		if details != nil && details.Reason == "max_output_tokens" {
			return "length"
		}
		return "stop"
	case "completed":
		if len(toolCalls) > 0 {
			return "tool_calls"
		}
		return "stop"
	default:
		return "stop"
	}
}

type ResponsesEventToChatState struct {
	ID                     string
	Model                  string
	Created                int64
	SentRole               bool
	SawToolCall            bool
	SawText                bool
	Finalized              bool
	NextToolCallIndex      int
	OutputIndexToToolIndex map[int]int
	IncludeUsage           bool
	Usage                  *ChatUsage
}

func NewResponsesEventToChatState() *ResponsesEventToChatState {
	return &ResponsesEventToChatState{
		ID:                     generateChatCmplID(),
		Created:                time.Now().Unix(),
		OutputIndexToToolIndex: make(map[int]int),
	}
}

func ResponsesEventToChatChunks(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	switch evt.Type {
	case "response.created":
		return resToChatHandleCreated(evt, state)
	case "response.output_text.delta":
		return resToChatHandleTextDelta(evt, state)
	case "response.output_item.added":
		return resToChatHandleOutputItemAdded(evt, state)
	case "response.function_call_arguments.delta":
		return resToChatHandleFuncArgsDelta(evt, state)
	case "response.reasoning_summary_text.delta":
		return resToChatHandleReasoningDelta(evt, state)
	case "response.reasoning_summary_text.done":
		return nil
	case "response.completed", "response.incomplete", "response.failed":
		return resToChatHandleCompleted(evt, state)
	default:
		return nil
	}
}

func resToChatHandleCreated(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Response != nil {
		if evt.Response.ID != "" {
			state.ID = evt.Response.ID
		}
		if state.Model == "" && evt.Response.Model != "" {
			state.Model = evt.Response.Model
		}
	}
	if state.SentRole {
		return nil
	}
	state.SentRole = true

	role := "assistant"
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{Role: role})}
}

func resToChatHandleTextDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}
	state.SawText = true
	content := evt.Delta
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{Content: &content})}
}

func resToChatHandleOutputItemAdded(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Item == nil || evt.Item.Type != "function_call" {
		return nil
	}

	state.SawToolCall = true
	idx := state.NextToolCallIndex
	state.OutputIndexToToolIndex[evt.OutputIndex] = idx
	state.NextToolCallIndex++

	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{
		ToolCalls: []ChatToolCall{{
			Index: &idx,
			ID:    evt.Item.CallID,
			Type:  "function",
			Function: ChatFunctionCall{
				Name: evt.Item.Name,
			},
		}},
	})}
}

func resToChatHandleFuncArgsDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}

	idx, ok := state.OutputIndexToToolIndex[evt.OutputIndex]
	if !ok {
		return nil
	}

	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{
		ToolCalls: []ChatToolCall{{
			Index: &idx,
			Function: ChatFunctionCall{
				Arguments: evt.Delta,
			},
		}},
	})}
}

func resToChatHandleReasoningDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}
	reasoning := evt.Delta
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{ReasoningContent: &reasoning})}
}

func resToChatHandleCompleted(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	state.Finalized = true
	finishReason := "stop"

	if evt.Response != nil {
		if evt.Response.Usage != nil {
			u := evt.Response.Usage
			usage := &ChatUsage{
				PromptTokens:     u.InputTokens,
				CompletionTokens: u.OutputTokens,
				TotalTokens:      u.InputTokens + u.OutputTokens,
			}
			if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens > 0 {
				usage.PromptTokensDetails = &ChatTokenDetails{
					CachedTokens: u.InputTokensDetails.CachedTokens,
				}
			}
			state.Usage = usage
		}

		switch evt.Response.Status {
		case "incomplete":
			if evt.Response.IncompleteDetails != nil && evt.Response.IncompleteDetails.Reason == "max_output_tokens" {
				finishReason = "length"
			}
		case "completed":
			if state.SawToolCall {
				finishReason = "tool_calls"
			}
		}
	} else if state.SawToolCall {
		finishReason = "tool_calls"
	}

	var chunks []ChatCompletionsChunk
	chunks = append(chunks, makeChatFinishChunk(state, finishReason))

	if state.IncludeUsage && state.Usage != nil {
		chunks = append(chunks, ChatCompletionsChunk{
			ID:      state.ID,
			Object:  "chat.completion.chunk",
			Created: state.Created,
			Model:   state.Model,
			Choices: []ChatChunkChoice{},
			Usage:   state.Usage,
		})
	}

	return chunks
}

func makeChatDeltaChunk(state *ResponsesEventToChatState, delta ChatDelta) ChatCompletionsChunk {
	return ChatCompletionsChunk{
		ID:      state.ID,
		Object:  "chat.completion.chunk",
		Created: state.Created,
		Model:   state.Model,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: nil,
		}},
	}
}

func makeChatFinishChunk(state *ResponsesEventToChatState, finishReason string) ChatCompletionsChunk {
	empty := ""
	return ChatCompletionsChunk{
		ID:      state.ID,
		Object:  "chat.completion.chunk",
		Created: state.Created,
		Model:   state.Model,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        ChatDelta{Content: &empty},
			FinishReason: &finishReason,
		}},
	}
}

func ChatChunkToSSE(chunk ChatCompletionsChunk) (string, error) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data: %s\n\n", data), nil
}

func generateChatCmplID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "chatcmpl-" + hex.EncodeToString(b)
}
