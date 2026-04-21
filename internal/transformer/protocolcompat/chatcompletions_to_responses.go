package protocolcompat

import (
	"encoding/json"
	"fmt"
	"strings"
)

type chatMessageContent struct {
	Text  *string
	Parts []ChatContentPart
}

// [fork] Ported from sub2api to keep Chat -> Responses request conversion
// consistent with the upstream gateway implementation.
func ChatCompletionsToResponses(req *ChatCompletionsRequest) (*ResponsesRequest, error) {
	input, err := convertChatMessagesToResponsesInput(req.Messages)
	if err != nil {
		return nil, err
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	out := &ResponsesRequest{
		Model:        req.Model,
		Instructions: req.Instructions,
		Input:        inputJSON,
		Temperature:  req.Temperature,
		TopP:         req.TopP,
		Stream:       true,
		Include:      []string{"reasoning.encrypted_content"},
		ServiceTier:  req.ServiceTier,
	}

	storeFalse := false
	out.Store = &storeFalse

	maxTokens := 0
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}
	if req.MaxCompletionTokens != nil {
		maxTokens = *req.MaxCompletionTokens
	}
	if maxTokens > 0 {
		v := maxTokens
		if v < minMaxOutputTokens {
			v = minMaxOutputTokens
		}
		out.MaxOutputTokens = &v
	}

	if req.ReasoningEffort != "" {
		out.Reasoning = &ResponsesReasoning{
			Effort:  req.ReasoningEffort,
			Summary: "auto",
		}
	}

	if len(req.Tools) > 0 || len(req.Functions) > 0 {
		out.Tools = convertChatToolsToResponses(req.Tools, req.Functions)
	}

	if len(req.ToolChoice) > 0 {
		out.ToolChoice = req.ToolChoice
	} else if len(req.FunctionCall) > 0 {
		tc, err := convertChatFunctionCallToToolChoice(req.FunctionCall)
		if err != nil {
			return nil, fmt.Errorf("convert function_call: %w", err)
		}
		out.ToolChoice = tc
	}

	return out, nil
}

func convertChatMessagesToResponsesInput(msgs []ChatMessage) ([]ResponsesInputItem, error) {
	var out []ResponsesInputItem
	for _, m := range msgs {
		items, err := chatMessageToResponsesItems(m)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func chatMessageToResponsesItems(m ChatMessage) ([]ResponsesInputItem, error) {
	switch m.Role {
	case "system":
		return chatSystemToResponses(m)
	case "user":
		return chatUserToResponses(m)
	case "assistant":
		return chatAssistantToResponses(m)
	case "tool":
		return chatToolToResponses(m)
	case "function":
		return chatFunctionToResponses(m)
	default:
		return chatUserToResponses(m)
	}
}

func chatSystemToResponses(m ChatMessage) ([]ResponsesInputItem, error) {
	parsed, err := parseChatMessageContent(m.Content)
	if err != nil {
		return nil, err
	}
	content, err := marshalChatInputContent(parsed)
	if err != nil {
		return nil, err
	}
	return []ResponsesInputItem{{Role: "system", Content: content}}, nil
}

func chatUserToResponses(m ChatMessage) ([]ResponsesInputItem, error) {
	parsed, err := parseChatMessageContent(m.Content)
	if err != nil {
		return nil, fmt.Errorf("parse user content: %w", err)
	}
	content, err := marshalChatInputContent(parsed)
	if err != nil {
		return nil, err
	}
	return []ResponsesInputItem{{Role: "user", Content: content}}, nil
}

func chatAssistantToResponses(m ChatMessage) ([]ResponsesInputItem, error) {
	var items []ResponsesInputItem

	if len(m.Content) > 0 {
		s, err := parseAssistantContent(m.Content)
		if err != nil {
			return nil, err
		}
		if s != "" {
			parts := []ResponsesContentPart{{Type: "output_text", Text: s}}
			partsJSON, err := json.Marshal(parts)
			if err != nil {
				return nil, err
			}
			items = append(items, ResponsesInputItem{Role: "assistant", Content: partsJSON})
		}
	}

	for _, tc := range m.ToolCalls {
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		items = append(items, ResponsesInputItem{
			Type:      "function_call",
			CallID:    tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return items, nil
}

func parseAssistantContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", nil
	}

	var b strings.Builder
	write := func(v string) error {
		_, err := b.WriteString(v)
		return err
	}
	for _, p := range parts {
		typ, _ := p["type"].(string)
		text, _ := p["text"].(string)
		thinking, _ := p["thinking"].(string)

		switch typ {
		case "thinking", "reasoning":
			if thinking != "" {
				if err := write("<thinking>"); err != nil {
					return "", err
				}
				if err := write(thinking); err != nil {
					return "", err
				}
				if err := write("</thinking>"); err != nil {
					return "", err
				}
			} else if text != "" {
				if err := write("<thinking>"); err != nil {
					return "", err
				}
				if err := write(text); err != nil {
					return "", err
				}
				if err := write("</thinking>"); err != nil {
					return "", err
				}
			}
		default:
			if text != "" {
				if err := write(text); err != nil {
					return "", err
				}
			}
		}
	}

	return b.String(), nil
}

func chatToolToResponses(m ChatMessage) ([]ResponsesInputItem, error) {
	output, err := parseChatContent(m.Content)
	if err != nil {
		return nil, err
	}
	if output == "" {
		output = "(empty)"
	}
	return []ResponsesInputItem{{
		Type:   "function_call_output",
		CallID: m.ToolCallID,
		Output: output,
	}}, nil
}

func chatFunctionToResponses(m ChatMessage) ([]ResponsesInputItem, error) {
	output, err := parseChatContent(m.Content)
	if err != nil {
		return nil, err
	}
	if output == "" {
		output = "(empty)"
	}
	return []ResponsesInputItem{{
		Type:   "function_call_output",
		CallID: m.Name,
		Output: output,
	}}, nil
}

func parseChatContent(raw json.RawMessage) (string, error) {
	parsed, err := parseChatMessageContent(raw)
	if err != nil {
		return "", err
	}
	if parsed.Text != nil {
		return *parsed.Text, nil
	}
	return flattenChatContentParts(parsed.Parts), nil
}

func parseChatMessageContent(raw json.RawMessage) (chatMessageContent, error) {
	if len(raw) == 0 {
		return chatMessageContent{Text: stringPtr("")}, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return chatMessageContent{Text: &s}, nil
	}

	var parts []ChatContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		return chatMessageContent{Parts: parts}, nil
	}

	return chatMessageContent{}, fmt.Errorf("parse content as string or parts array")
}

func marshalChatInputContent(content chatMessageContent) (json.RawMessage, error) {
	if content.Text != nil {
		return json.Marshal(*content.Text)
	}
	return json.Marshal(convertChatContentPartsToResponses(content.Parts))
}

func convertChatContentPartsToResponses(parts []ChatContentPart) []ResponsesContentPart {
	var responseParts []ResponsesContentPart
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Text != "" {
				responseParts = append(responseParts, ResponsesContentPart{
					Type: "input_text",
					Text: p.Text,
				})
			}
		case "image_url":
			if p.ImageURL != nil && p.ImageURL.URL != "" && !isEmptyBase64DataURI(p.ImageURL.URL) {
				responseParts = append(responseParts, ResponsesContentPart{
					Type:     "input_image",
					ImageURL: p.ImageURL.URL,
				})
			}
		}
	}
	return responseParts
}

func isEmptyBase64DataURI(raw string) bool {
	if !strings.HasPrefix(raw, "data:") {
		return false
	}
	rest := strings.TrimPrefix(raw, "data:")
	semicolonIdx := strings.Index(rest, ";")
	if semicolonIdx < 0 {
		return false
	}
	rest = rest[semicolonIdx+1:]
	if !strings.HasPrefix(rest, "base64,") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, "base64,")) == ""
}

func flattenChatContentParts(parts []ChatContentPart) string {
	var textParts []string
	for _, p := range parts {
		if p.Type == "text" && p.Text != "" {
			textParts = append(textParts, p.Text)
		}
	}
	return strings.Join(textParts, "")
}

func stringPtr(s string) *string {
	return &s
}

func convertChatToolsToResponses(tools []ChatTool, functions []ChatFunction) []ResponsesTool {
	var out []ResponsesTool

	for _, t := range tools {
		if t.Type != "function" || t.Function == nil {
			continue
		}
		out = append(out, ResponsesTool{
			Type:        "function",
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
			Strict:      t.Function.Strict,
		})
	}

	for _, f := range functions {
		out = append(out, ResponsesTool{
			Type:        "function",
			Name:        f.Name,
			Description: f.Description,
			Parameters:  f.Parameters,
			Strict:      f.Strict,
		})
	}

	return out
}

func convertChatFunctionCallToToolChoice(raw json.RawMessage) (json.RawMessage, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return json.Marshal(s)
	}

	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"type":     "function",
		"function": map[string]string{"name": obj.Name},
	})
}
