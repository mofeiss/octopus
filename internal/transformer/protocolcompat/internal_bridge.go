package protocolcompat

import (
	"encoding/json"
	"fmt"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// [fork] Reuse JSON round-tripping because Octopus internal responses are
// intentionally chat-completions-shaped, which keeps the bridge minimal.

func ChatResponseToInternal(resp *ChatCompletionsResponse) (*model.InternalLLMResponse, error) {
	if resp == nil {
		return nil, fmt.Errorf("chat response is nil")
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal chat response: %w", err)
	}

	var internal model.InternalLLMResponse
	if err := json.Unmarshal(data, &internal); err != nil {
		return nil, fmt.Errorf("unmarshal internal response: %w", err)
	}
	return &internal, nil
}

func ChatChunkToInternal(chunk *ChatCompletionsChunk) (*model.InternalLLMResponse, error) {
	if chunk == nil {
		return nil, fmt.Errorf("chat chunk is nil")
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return nil, fmt.Errorf("marshal chat chunk: %w", err)
	}

	var internal model.InternalLLMResponse
	if err := json.Unmarshal(data, &internal); err != nil {
		return nil, fmt.Errorf("unmarshal internal chunk: %w", err)
	}
	return &internal, nil
}
