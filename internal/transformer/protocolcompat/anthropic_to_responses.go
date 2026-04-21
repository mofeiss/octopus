package protocolcompat

import (
	"encoding/json"
	"fmt"
	"strings"
)

// [fork] Ported from sub2api to keep Anthropic -> Responses request conversion
// aligned with the upstream gateway behavior.
func AnthropicToResponses(req *AnthropicRequest) (*ResponsesRequest, error) {
	input, err := convertAnthropicToResponsesInput(req.System, req.Messages)
	if err != nil {
		return nil, err
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	out := &ResponsesRequest{
		Model:       req.Model,
		Input:       inputJSON,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		Include:     []string{"reasoning.encrypted_content"},
	}

	storeFalse := false
	out.Store = &storeFalse

	if req.MaxTokens > 0 {
		v := req.MaxTokens
		if v < minMaxOutputTokens {
			v = minMaxOutputTokens
		}
		out.MaxOutputTokens = &v
	}

	effort := "high"
	if req.OutputConfig != nil && req.OutputConfig.Effort != "" {
		effort = req.OutputConfig.Effort
	}
	out.Reasoning = &ResponsesReasoning{
		Effort:  mapAnthropicEffortToResponses(effort),
		Summary: "auto",
	}

	if len(req.ToolChoice) > 0 {
		tc, err := convertAnthropicToolChoiceToResponses(req.ToolChoice)
		if err != nil {
			return nil, fmt.Errorf("convert tool_choice: %w", err)
		}
		out.ToolChoice = tc
	}

	if len(req.Tools) > 0 {
		out.Tools = convertAnthropicToolsToResponses(req.Tools)
	}

	return out, nil
}

func convertAnthropicToolChoiceToResponses(raw json.RawMessage) (json.RawMessage, error) {
	var tc struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		return nil, err
	}

	switch tc.Type {
	case "auto":
		return json.Marshal("auto")
	case "any":
		return json.Marshal("required")
	case "none":
		return json.Marshal("none")
	case "tool":
		return json.Marshal(map[string]any{
			"type":     "function",
			"function": map[string]string{"name": tc.Name},
		})
	default:
		return raw, nil
	}
}

func convertAnthropicToResponsesInput(system json.RawMessage, msgs []AnthropicMessage) ([]ResponsesInputItem, error) {
	var out []ResponsesInputItem

	if len(system) > 0 {
		sysText, err := parseAnthropicSystemPrompt(system)
		if err != nil {
			return nil, err
		}
		if sysText != "" {
			content, _ := json.Marshal(sysText)
			out = append(out, ResponsesInputItem{
				Role:    "system",
				Content: content,
			})
		}
	}

	for _, m := range msgs {
		items, err := anthropicMsgToResponsesItems(m)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func parseAnthropicSystemPrompt(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", err
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func anthropicMsgToResponsesItems(m AnthropicMessage) ([]ResponsesInputItem, error) {
	switch m.Role {
	case "user":
		return anthropicUserToResponses(m.Content)
	case "assistant":
		return anthropicAssistantToResponses(m.Content)
	default:
		return anthropicUserToResponses(m.Content)
	}
}

func anthropicUserToResponses(raw json.RawMessage) ([]ResponsesInputItem, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		content, _ := json.Marshal(s)
		return []ResponsesInputItem{{Role: "user", Content: content}}, nil
	}

	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}

	var out []ResponsesInputItem
	var toolResultImageParts []ResponsesContentPart

	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		outputText, imageParts := convertToolResultOutput(b)
		out = append(out, ResponsesInputItem{
			Type:   "function_call_output",
			CallID: toResponsesCallID(b.ToolUseID),
			Output: outputText,
		})
		toolResultImageParts = append(toolResultImageParts, imageParts...)
	}

	var parts []ResponsesContentPart
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, ResponsesContentPart{Type: "input_text", Text: b.Text})
			}
		case "image":
			if uri := anthropicImageToDataURI(b.Source); uri != "" {
				parts = append(parts, ResponsesContentPart{Type: "input_image", ImageURL: uri})
			}
		}
	}
	parts = append(parts, toolResultImageParts...)

	if len(parts) > 0 {
		content, err := json.Marshal(parts)
		if err != nil {
			return nil, err
		}
		out = append(out, ResponsesInputItem{Role: "user", Content: content})
	}

	return out, nil
}

func anthropicAssistantToResponses(raw json.RawMessage) ([]ResponsesInputItem, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		parts := []ResponsesContentPart{{Type: "output_text", Text: s}}
		partsJSON, err := json.Marshal(parts)
		if err != nil {
			return nil, err
		}
		return []ResponsesInputItem{{Role: "assistant", Content: partsJSON}}, nil
	}

	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}

	var items []ResponsesInputItem

	text := extractAnthropicTextFromBlocks(blocks)
	if text != "" {
		parts := []ResponsesContentPart{{Type: "output_text", Text: text}}
		partsJSON, err := json.Marshal(parts)
		if err != nil {
			return nil, err
		}
		items = append(items, ResponsesInputItem{Role: "assistant", Content: partsJSON})
	}

	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		args := "{}"
		if len(b.Input) > 0 {
			args = string(b.Input)
		}
		items = append(items, ResponsesInputItem{
			Type:      "function_call",
			CallID:    toResponsesCallID(b.ID),
			Name:      b.Name,
			Arguments: args,
		})
	}

	return items, nil
}

func toResponsesCallID(id string) string {
	if strings.HasPrefix(id, "fc_") {
		return id
	}
	return "fc_" + id
}

func fromResponsesCallID(id string) string {
	if after, ok := strings.CutPrefix(id, "fc_"); ok {
		if strings.HasPrefix(after, "toolu_") || strings.HasPrefix(after, "call_") {
			return after
		}
	}
	return id
}

func anthropicImageToDataURI(src *AnthropicImageSource) string {
	if src == nil || src.Data == "" {
		return ""
	}
	mediaType := src.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}
	return "data:" + mediaType + ";base64," + src.Data
}

func convertToolResultOutput(b AnthropicContentBlock) (string, []ResponsesContentPart) {
	if len(b.Content) == 0 {
		return "(empty)", nil
	}

	var s string
	if err := json.Unmarshal(b.Content, &s); err == nil {
		if s == "" {
			s = "(empty)"
		}
		return s, nil
	}

	var inner []AnthropicContentBlock
	if err := json.Unmarshal(b.Content, &inner); err != nil {
		return "(empty)", nil
	}

	var textParts []string
	var imageParts []ResponsesContentPart
	for _, ib := range inner {
		switch ib.Type {
		case "text":
			if ib.Text != "" {
				textParts = append(textParts, ib.Text)
			}
		case "image":
			if uri := anthropicImageToDataURI(ib.Source); uri != "" {
				imageParts = append(imageParts, ResponsesContentPart{Type: "input_image", ImageURL: uri})
			}
		}
	}

	text := strings.Join(textParts, "\n\n")
	if text == "" {
		text = "(empty)"
	}
	return text, imageParts
}

func extractAnthropicTextFromBlocks(blocks []AnthropicContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func mapAnthropicEffortToResponses(effort string) string {
	if effort == "max" {
		return "xhigh"
	}
	return effort
}

func convertAnthropicToolsToResponses(tools []AnthropicTool) []ResponsesTool {
	var out []ResponsesTool
	for _, t := range tools {
		if strings.HasPrefix(t.Type, "web_search") {
			out = append(out, ResponsesTool{Type: "web_search"})
			continue
		}
		out = append(out, ResponsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  normalizeToolParameters(t.InputSchema),
		})
	}
	return out
}

func normalizeToolParameters(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 || string(schema) == "null" {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(schema, &m); err != nil {
		return schema
	}

	typ := m["type"]
	if string(typ) != `"object"` {
		return schema
	}
	if _, ok := m["properties"]; ok {
		return schema
	}

	m["properties"] = json.RawMessage(`{}`)
	out, err := json.Marshal(m)
	if err != nil {
		return schema
	}
	return out
}
