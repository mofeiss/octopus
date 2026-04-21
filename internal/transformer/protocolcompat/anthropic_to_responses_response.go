package protocolcompat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// [fork] Ported from sub2api to keep Anthropic -> Responses response and
// streaming conversion aligned with the upstream gateway behavior.
func AnthropicToResponsesResponse(resp *AnthropicResponse) *ResponsesResponse {
	id := resp.ID
	if id == "" {
		id = generateResponsesID()
	}

	out := &ResponsesResponse{
		ID:     id,
		Object: "response",
		Model:  resp.Model,
	}

	var outputs []ResponsesOutput
	var msgParts []ResponsesContentPart

	for _, block := range resp.Content {
		switch block.Type {
		case "thinking":
			if block.Thinking != "" {
				outputs = append(outputs, ResponsesOutput{
					Type: "reasoning",
					ID:   generateItemID(),
					Summary: []ResponsesSummary{{
						Type: "summary_text",
						Text: block.Thinking,
					}},
				})
			}
		case "text":
			if block.Text != "" {
				msgParts = append(msgParts, ResponsesContentPart{
					Type: "output_text",
					Text: block.Text,
				})
			}
		case "tool_use":
			args := "{}"
			if len(block.Input) > 0 {
				args = string(block.Input)
			}
			outputs = append(outputs, ResponsesOutput{
				Type:      "function_call",
				ID:        generateItemID(),
				CallID:    toResponsesCallID(block.ID),
				Name:      block.Name,
				Arguments: args,
				Status:    "completed",
			})
		}
	}

	if len(msgParts) > 0 {
		outputs = append(outputs, ResponsesOutput{
			Type:    "message",
			ID:      generateItemID(),
			Role:    "assistant",
			Content: msgParts,
			Status:  "completed",
		})
	}

	if len(outputs) == 0 {
		outputs = append(outputs, ResponsesOutput{
			Type:    "message",
			ID:      generateItemID(),
			Role:    "assistant",
			Content: []ResponsesContentPart{{Type: "output_text", Text: ""}},
			Status:  "completed",
		})
	}
	out.Output = outputs

	out.Status = anthropicStopReasonToResponsesStatus(resp.StopReason, resp.Content)
	if out.Status == "incomplete" {
		out.IncompleteDetails = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	}

	out.Usage = &ResponsesUsage{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	if resp.Usage.CacheReadInputTokens > 0 {
		out.Usage.InputTokensDetails = &ResponsesInputTokensDetails{
			CachedTokens: resp.Usage.CacheReadInputTokens,
		}
	}

	return out
}

func anthropicStopReasonToResponsesStatus(stopReason string, blocks []AnthropicContentBlock) string {
	switch stopReason {
	case "max_tokens":
		return "incomplete"
	case "end_turn", "tool_use", "stop_sequence":
		return "completed"
	default:
		return "completed"
	}
}

type AnthropicEventToResponsesState struct {
	ResponseID     string
	Model          string
	Created        int64
	SequenceNumber int

	CreatedSent   bool
	CompletedSent bool

	OutputIndex     int
	CurrentItemID   string
	CurrentItemType string

	ContentIndex int

	CurrentCallID string
	CurrentName   string

	InputTokens          int
	OutputTokens         int
	CacheReadInputTokens int
}

func NewAnthropicEventToResponsesState() *AnthropicEventToResponsesState {
	return &AnthropicEventToResponsesState{
		Created: time.Now().Unix(),
	}
}

func AnthropicEventToResponsesEvents(
	evt *AnthropicStreamEvent,
	state *AnthropicEventToResponsesState,
) []ResponsesStreamEvent {
	switch evt.Type {
	case "message_start":
		return anthToResHandleMessageStart(evt, state)
	case "content_block_start":
		return anthToResHandleContentBlockStart(evt, state)
	case "content_block_delta":
		return anthToResHandleContentBlockDelta(evt, state)
	case "content_block_stop":
		return anthToResHandleContentBlockStop(evt, state)
	case "message_delta":
		return anthToResHandleMessageDelta(evt, state)
	case "message_stop":
		return anthToResHandleMessageStop(state)
	default:
		return nil
	}
}

func anthToResHandleMessageStart(evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if evt.Message != nil {
		state.ResponseID = evt.Message.ID
		if state.Model == "" {
			state.Model = evt.Message.Model
		}
		if evt.Message.Usage.InputTokens > 0 {
			state.InputTokens = evt.Message.Usage.InputTokens
		}
	}

	if state.CreatedSent {
		return nil
	}
	state.CreatedSent = true

	return []ResponsesStreamEvent{makeResponsesCreatedEvent(state)}
}

func anthToResHandleContentBlockStart(evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if evt.ContentBlock == nil {
		return nil
	}

	var events []ResponsesStreamEvent

	switch evt.ContentBlock.Type {
	case "thinking":
		state.CurrentItemID = generateItemID()
		state.CurrentItemType = "reasoning"
		state.ContentIndex = 0

		events = append(events, makeResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
			OutputIndex: state.OutputIndex,
			Item: &ResponsesOutput{
				Type: "reasoning",
				ID:   state.CurrentItemID,
			},
		}))
	case "text":
		if state.CurrentItemType != "message" {
			state.CurrentItemID = generateItemID()
			state.CurrentItemType = "message"
			state.ContentIndex = 0

			events = append(events, makeResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
				OutputIndex: state.OutputIndex,
				Item: &ResponsesOutput{
					Type:   "message",
					ID:     state.CurrentItemID,
					Role:   "assistant",
					Status: "in_progress",
				},
			}))
		}
	case "tool_use":
		events = append(events, closeCurrentResponsesItem(state)...)

		state.CurrentItemID = generateItemID()
		state.CurrentItemType = "function_call"
		state.CurrentCallID = toResponsesCallID(evt.ContentBlock.ID)
		state.CurrentName = evt.ContentBlock.Name

		events = append(events, makeResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
			OutputIndex: state.OutputIndex,
			Item: &ResponsesOutput{
				Type:   "function_call",
				ID:     state.CurrentItemID,
				CallID: state.CurrentCallID,
				Name:   state.CurrentName,
				Status: "in_progress",
			},
		}))
	}

	return events
}

func anthToResHandleContentBlockDelta(evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if evt.Delta == nil {
		return nil
	}

	switch evt.Delta.Type {
	case "text_delta":
		if evt.Delta.Text == "" {
			return nil
		}
		return []ResponsesStreamEvent{makeResponsesEvent(state, "response.output_text.delta", &ResponsesStreamEvent{
			OutputIndex:  state.OutputIndex,
			ContentIndex: state.ContentIndex,
			Delta:        evt.Delta.Text,
			ItemID:       state.CurrentItemID,
		})}
	case "thinking_delta":
		if evt.Delta.Thinking == "" {
			return nil
		}
		return []ResponsesStreamEvent{makeResponsesEvent(state, "response.reasoning_summary_text.delta", &ResponsesStreamEvent{
			OutputIndex:  state.OutputIndex,
			SummaryIndex: 0,
			Delta:        evt.Delta.Thinking,
			ItemID:       state.CurrentItemID,
		})}
	case "input_json_delta":
		if evt.Delta.PartialJSON == "" {
			return nil
		}
		return []ResponsesStreamEvent{makeResponsesEvent(state, "response.function_call_arguments.delta", &ResponsesStreamEvent{
			OutputIndex: state.OutputIndex,
			Delta:       evt.Delta.PartialJSON,
			ItemID:      state.CurrentItemID,
			CallID:      state.CurrentCallID,
			Name:        state.CurrentName,
		})}
	case "signature_delta":
		return nil
	}

	return nil
}

func anthToResHandleContentBlockStop(evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	switch state.CurrentItemType {
	case "reasoning":
		events := []ResponsesStreamEvent{
			makeResponsesEvent(state, "response.reasoning_summary_text.done", &ResponsesStreamEvent{
				OutputIndex:  state.OutputIndex,
				SummaryIndex: 0,
				ItemID:       state.CurrentItemID,
			}),
		}
		events = append(events, closeCurrentResponsesItem(state)...)
		return events
	case "function_call":
		events := []ResponsesStreamEvent{
			makeResponsesEvent(state, "response.function_call_arguments.done", &ResponsesStreamEvent{
				OutputIndex: state.OutputIndex,
				ItemID:      state.CurrentItemID,
				CallID:      state.CurrentCallID,
				Name:        state.CurrentName,
			}),
		}
		events = append(events, closeCurrentResponsesItem(state)...)
		return events
	case "message":
		return []ResponsesStreamEvent{
			makeResponsesEvent(state, "response.output_text.done", &ResponsesStreamEvent{
				OutputIndex:  state.OutputIndex,
				ContentIndex: state.ContentIndex,
				ItemID:       state.CurrentItemID,
			}),
		}
	}

	return nil
}

func anthToResHandleMessageDelta(evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if evt.Usage != nil {
		state.OutputTokens = evt.Usage.OutputTokens
		if evt.Usage.CacheReadInputTokens > 0 {
			state.CacheReadInputTokens = evt.Usage.CacheReadInputTokens
		}
	}
	return nil
}

func anthToResHandleMessageStop(state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if state.CompletedSent {
		return nil
	}

	var events []ResponsesStreamEvent
	events = append(events, closeCurrentResponsesItem(state)...)
	events = append(events, makeResponsesCompletedEvent(state, "completed", nil))
	state.CompletedSent = true
	return events
}

func closeCurrentResponsesItem(state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if state.CurrentItemType == "" {
		return nil
	}

	itemType := state.CurrentItemType
	itemID := state.CurrentItemID

	state.CurrentItemType = ""
	state.CurrentItemID = ""
	state.CurrentCallID = ""
	state.CurrentName = ""
	state.OutputIndex++
	state.ContentIndex = 0

	return []ResponsesStreamEvent{makeResponsesEvent(state, "response.output_item.done", &ResponsesStreamEvent{
		OutputIndex: state.OutputIndex - 1,
		Item: &ResponsesOutput{
			Type:   itemType,
			ID:     itemID,
			Status: "completed",
		},
	})}
}

func makeResponsesCreatedEvent(state *AnthropicEventToResponsesState) ResponsesStreamEvent {
	seq := state.SequenceNumber
	state.SequenceNumber++
	return ResponsesStreamEvent{
		Type:           "response.created",
		SequenceNumber: seq,
		Response: &ResponsesResponse{
			ID:     state.ResponseID,
			Object: "response",
			Model:  state.Model,
			Status: "in_progress",
			Output: []ResponsesOutput{},
		},
	}
}

func makeResponsesCompletedEvent(
	state *AnthropicEventToResponsesState,
	status string,
	incompleteDetails *ResponsesIncompleteDetails,
) ResponsesStreamEvent {
	seq := state.SequenceNumber
	state.SequenceNumber++

	usage := &ResponsesUsage{
		InputTokens:  state.InputTokens,
		OutputTokens: state.OutputTokens,
		TotalTokens:  state.InputTokens + state.OutputTokens,
	}
	if state.CacheReadInputTokens > 0 {
		usage.InputTokensDetails = &ResponsesInputTokensDetails{
			CachedTokens: state.CacheReadInputTokens,
		}
	}

	return ResponsesStreamEvent{
		Type:           "response.completed",
		SequenceNumber: seq,
		Response: &ResponsesResponse{
			ID:                state.ResponseID,
			Object:            "response",
			Model:             state.Model,
			Status:            status,
			Output:            []ResponsesOutput{},
			Usage:             usage,
			IncompleteDetails: incompleteDetails,
		},
	}
}

func makeResponsesEvent(state *AnthropicEventToResponsesState, eventType string, template *ResponsesStreamEvent) ResponsesStreamEvent {
	seq := state.SequenceNumber
	state.SequenceNumber++

	evt := *template
	evt.Type = eventType
	evt.SequenceNumber = seq
	return evt
}

func generateResponsesID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "resp_" + hex.EncodeToString(b)
}

func generateItemID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "item_" + hex.EncodeToString(b)
}

func ResponsesEventToSSE(evt ResponsesStreamEvent) (string, error) {
	data, err := json.Marshal(evt)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", evt.Type, data), nil
}
