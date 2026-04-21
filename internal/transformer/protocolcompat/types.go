package protocolcompat

import "encoding/json"

// [fork] This package ports the protocol conversion state machines from
// sub2api so Octopus can reuse the mature Responses / Chat / Anthropic
// conversion logic on cross-protocol relay paths.

type AnthropicRequest struct {
	Model        string                 `json:"model"`
	MaxTokens    int                    `json:"max_tokens"`
	System       json.RawMessage        `json:"system,omitempty"`
	Messages     []AnthropicMessage     `json:"messages"`
	Tools        []AnthropicTool        `json:"tools,omitempty"`
	Stream       bool                   `json:"stream,omitempty"`
	Temperature  *float64               `json:"temperature,omitempty"`
	TopP         *float64               `json:"top_p,omitempty"`
	StopSeqs     []string               `json:"stop_sequences,omitempty"`
	Thinking     *AnthropicThinking     `json:"thinking,omitempty"`
	ToolChoice   json.RawMessage        `json:"tool_choice,omitempty"`
	OutputConfig *AnthropicOutputConfig `json:"output_config,omitempty"`
}

type AnthropicOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

type AnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type AnthropicContentBlock struct {
	Type string `json:"type"`

	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`

	Source *AnthropicImageSource `json:"source,omitempty"`

	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`

	Signature string `json:"signature,omitempty"`
	Action    any    `json:"action,omitempty"`
	Status    string `json:"status,omitempty"`
}

type AnthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type AnthropicTool struct {
	Type        string          `json:"type,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type AnthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage          `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type AnthropicStreamEvent struct {
	Type string `json:"type"`

	Message *AnthropicResponse `json:"message,omitempty"`

	Index        *int                   `json:"index,omitempty"`
	ContentBlock *AnthropicContentBlock `json:"content_block,omitempty"`

	Delta *AnthropicDelta `json:"delta,omitempty"`

	Usage *AnthropicUsage `json:"usage,omitempty"`
}

type AnthropicDelta struct {
	Type string `json:"type,omitempty"`

	Text string `json:"text,omitempty"`

	PartialJSON string `json:"partial_json,omitempty"`

	Thinking string `json:"thinking,omitempty"`

	Signature string `json:"signature,omitempty"`

	StopReason   string  `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

type ResponsesRequest struct {
	Model           string              `json:"model"`
	Instructions    string              `json:"instructions,omitempty"`
	Input           json.RawMessage     `json:"input"`
	MaxOutputTokens *int                `json:"max_output_tokens,omitempty"`
	Temperature     *float64            `json:"temperature,omitempty"`
	TopP            *float64            `json:"top_p,omitempty"`
	Stream          bool                `json:"stream,omitempty"`
	Tools           []ResponsesTool     `json:"tools,omitempty"`
	Include         []string            `json:"include,omitempty"`
	Store           *bool               `json:"store,omitempty"`
	Reasoning       *ResponsesReasoning `json:"reasoning,omitempty"`
	ToolChoice      json.RawMessage     `json:"tool_choice,omitempty"`
	ServiceTier     string              `json:"service_tier,omitempty"`
}

type ResponsesReasoning struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}

type ResponsesInputItem struct {
	Type string `json:"type,omitempty"`

	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`

	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	ID        string `json:"id,omitempty"`

	Output string `json:"output,omitempty"`
}

type ResponsesContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type ResponsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type ResponsesResponse struct {
	ID     string            `json:"id"`
	Object string            `json:"object"`
	Model  string            `json:"model"`
	Status string            `json:"status"`
	Output []ResponsesOutput `json:"output"`
	Usage  *ResponsesUsage   `json:"usage,omitempty"`

	IncompleteDetails *ResponsesIncompleteDetails `json:"incomplete_details,omitempty"`
	Error             *ResponsesError             `json:"error,omitempty"`
}

type ResponsesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ResponsesIncompleteDetails struct {
	Reason string `json:"reason"`
}

type ResponsesOutput struct {
	Type string `json:"type"`

	ID      string                 `json:"id,omitempty"`
	Role    string                 `json:"role,omitempty"`
	Content []ResponsesContentPart `json:"content,omitempty"`
	Status  string                 `json:"status,omitempty"`

	EncryptedContent string             `json:"encrypted_content,omitempty"`
	Summary          []ResponsesSummary `json:"summary,omitempty"`

	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	Action *WebSearchAction `json:"action,omitempty"`
}

type WebSearchAction struct {
	Type  string `json:"type,omitempty"`
	Query string `json:"query,omitempty"`
}

type ResponsesSummary struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`

	InputTokensDetails  *ResponsesInputTokensDetails  `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *ResponsesOutputTokensDetails `json:"output_tokens_details,omitempty"`
}

type ResponsesInputTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

type ResponsesOutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

type ResponsesStreamEvent struct {
	Type string `json:"type"`

	Response *ResponsesResponse `json:"response,omitempty"`

	Item *ResponsesOutput `json:"item,omitempty"`

	OutputIndex  int    `json:"output_index,omitempty"`
	ContentIndex int    `json:"content_index,omitempty"`
	Delta        string `json:"delta,omitempty"`
	Text         string `json:"text,omitempty"`
	ItemID       string `json:"item_id,omitempty"`

	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	SummaryIndex int `json:"summary_index,omitempty"`

	Code  string `json:"code,omitempty"`
	Param string `json:"param,omitempty"`

	SequenceNumber int `json:"sequence_number,omitempty"`
}

type ChatCompletionsRequest struct {
	Model               string             `json:"model"`
	Messages            []ChatMessage      `json:"messages"`
	Instructions        string             `json:"instructions,omitempty"`
	MaxTokens           *int               `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int               `json:"max_completion_tokens,omitempty"`
	Temperature         *float64           `json:"temperature,omitempty"`
	TopP                *float64           `json:"top_p,omitempty"`
	Stream              bool               `json:"stream,omitempty"`
	StreamOptions       *ChatStreamOptions `json:"stream_options,omitempty"`
	Tools               []ChatTool         `json:"tools,omitempty"`
	ToolChoice          json.RawMessage    `json:"tool_choice,omitempty"`
	ReasoningEffort     string             `json:"reasoning_effort,omitempty"`
	ServiceTier         string             `json:"service_tier,omitempty"`
	Stop                json.RawMessage    `json:"stop,omitempty"`

	Functions    []ChatFunction  `json:"functions,omitempty"`
	FunctionCall json.RawMessage `json:"function_call,omitempty"`
}

type ChatStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type ChatMessage struct {
	Role             string            `json:"role"`
	Content          json.RawMessage   `json:"content,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	Name             string            `json:"name,omitempty"`
	ToolCalls        []ChatToolCall    `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	FunctionCall     *ChatFunctionCall `json:"function_call,omitempty"`
}

type ChatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ChatImageURL `json:"image_url,omitempty"`
}

type ChatImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type ChatTool struct {
	Type     string        `json:"type"`
	Function *ChatFunction `json:"function,omitempty"`
}

type ChatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type ChatToolCall struct {
	Index    *int             `json:"index,omitempty"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ChatFunctionCall `json:"function"`
}

type ChatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatCompletionsResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"`
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	Choices           []ChatChoice `json:"choices"`
	Usage             *ChatUsage   `json:"usage,omitempty"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
	ServiceTier       string       `json:"service_tier,omitempty"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type ChatUsage struct {
	PromptTokens        int               `json:"prompt_tokens"`
	CompletionTokens    int               `json:"completion_tokens"`
	TotalTokens         int               `json:"total_tokens"`
	PromptTokensDetails *ChatTokenDetails `json:"prompt_tokens_details,omitempty"`
}

type ChatTokenDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

type ChatCompletionsChunk struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	Created           int64             `json:"created"`
	Model             string            `json:"model"`
	Choices           []ChatChunkChoice `json:"choices"`
	Usage             *ChatUsage        `json:"usage,omitempty"`
	SystemFingerprint string            `json:"system_fingerprint,omitempty"`
	ServiceTier       string            `json:"service_tier,omitempty"`
}

type ChatChunkChoice struct {
	Index        int       `json:"index"`
	Delta        ChatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type ChatDelta struct {
	Role             string         `json:"role,omitempty"`
	Content          *string        `json:"content,omitempty"`
	ReasoningContent *string        `json:"reasoning_content,omitempty"`
	ToolCalls        []ChatToolCall `json:"tool_calls,omitempty"`
}

const minMaxOutputTokens = 128
