package relay

import (
	"bytes"
	"io"
	"net/http"

	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

// [fork] snapshotHTTPRequestBody safely captures the actual outbound body without consuming it.
func snapshotHTTPRequestBody(req *http.Request) string {
	if req == nil || req.Body == nil {
		return ""
	}

	if req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			defer body.Close()
			data, readErr := io.ReadAll(body)
			if readErr == nil {
				return string(data)
			}
		}
	}

	data, err := io.ReadAll(req.Body)
	if err != nil {
		return ""
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	return string(data)
}

// [fork] captureResponseWriter records the exact body written back to the inbound client.
type captureResponseWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *captureResponseWriter) Write(data []byte) (int, error) {
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

func (w *captureResponseWriter) WriteString(data string) (int, error) {
	w.body.WriteString(data)
	return w.ResponseWriter.WriteString(data)
}

func (w *captureResponseWriter) CapturedBody() string {
	return w.body.String()
}

// [fork] fill relay log protocol display names from the inbound endpoint semantics.
func rawAPIFormatFromInboundType(inboundType inbound.InboundType) transformerModel.APIFormat {
	switch inboundType {
	case inbound.InboundTypeOpenAIChat:
		return transformerModel.APIFormatOpenAIChatCompletion
	case inbound.InboundTypeOpenAIResponse:
		return transformerModel.APIFormatOpenAIResponse
	case inbound.InboundTypeAnthropic:
		return transformerModel.APIFormatAnthropicMessage
	case inbound.InboundTypeGemini:
		return transformerModel.APIFormatGeminiContents
	case inbound.InboundTypeOpenAIEmbedding:
		return transformerModel.APIFormatOpenAIEmbedding
	default:
		return ""
	}
}

func relayProtocolNameFromAPIFormat(format transformerModel.APIFormat) string {
	switch format {
	case transformerModel.APIFormatOpenAIChatCompletion:
		return "OpenAI Chat"
	case transformerModel.APIFormatOpenAIResponse:
		return "OpenAI Responses"
	case transformerModel.APIFormatOpenAIImageGeneration:
		return "OpenAI Images"
	case transformerModel.APIFormatOpenAIEmbedding:
		return "OpenAI Embeddings"
	case transformerModel.APIFormatAnthropicMessage:
		return "Anthropic Messages"
	case transformerModel.APIFormatGeminiContents:
		return "Gemini"
	default:
		return ""
	}
}

func relayProtocolNameFromOutboundType(outboundType outbound.OutboundType) string {
	switch outboundType {
	case outbound.OutboundTypeOpenAIChat:
		return "OpenAI Chat"
	case outbound.OutboundTypeOpenAIResponse:
		return "OpenAI Responses"
	case outbound.OutboundTypeOpenAIEmbedding:
		return "OpenAI Embeddings"
	case outbound.OutboundTypeAnthropic:
		return "Anthropic Messages"
	case outbound.OutboundTypeGemini:
		return "Gemini"
	case outbound.OutboundTypeVolcengine:
		return "Volcengine Responses"
	default:
		return ""
	}
}
