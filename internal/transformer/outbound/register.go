package outbound

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound/authropic"
	"github.com/bestruirui/octopus/internal/transformer/outbound/gemini"
	"github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/transformer/outbound/volcengine"
)

type OutboundType int

const (
	OutboundTypeOpenAIChat OutboundType = iota
	OutboundTypeOpenAIResponse
	OutboundTypeAnthropic
	OutboundTypeGemini
	OutboundTypeVolcengine
	OutboundTypeOpenAIEmbedding
	OutboundTypeAuto // [fork] 根据请求端点自动推导实际出站类型
)

// EmbeddingChannelTypes 定义支持 embedding 请求的 channel 类型集合
var EmbeddingChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIEmbedding: true,
}

// ChatChannelTypes 定义支持 chat 请求的 channel 类型集合
var ChatChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIChat:     true,
	OutboundTypeOpenAIResponse: true,
	OutboundTypeAnthropic:      true,
	OutboundTypeGemini:         true,
	OutboundTypeVolcengine:     true,
}

// IsEmbeddingChannelType 判断 channel 类型是否支持 embedding 请求
func IsEmbeddingChannelType(channelType OutboundType) bool {
	return EmbeddingChannelTypes[channelType]
}

// IsChatChannelType 判断 channel 类型是否支持 chat 请求
func IsChatChannelType(channelType OutboundType) bool {
	return ChatChannelTypes[channelType]
}

var outboundFactories = map[OutboundType]func() model.Outbound{
	OutboundTypeOpenAIChat:      func() model.Outbound { return &openai.ChatOutbound{} },
	OutboundTypeOpenAIResponse:  func() model.Outbound { return &openai.ResponseOutbound{} },
	OutboundTypeOpenAIEmbedding: func() model.Outbound { return &openai.EmbeddingOutbound{} },
	OutboundTypeAnthropic:       func() model.Outbound { return &authropic.MessageOutbound{} },
	OutboundTypeGemini:          func() model.Outbound { return &gemini.MessagesOutbound{} },
	OutboundTypeVolcengine:      func() model.Outbound { return &volcengine.ResponseOutbound{} },
}

func Get(outboundType OutboundType) model.Outbound {
	if factory, ok := outboundFactories[outboundType]; ok {
		return factory()
	}
	return nil
}

// [fork] ResolveAutoByInboundType 根据请求端点语义，把 auto 渠道解析成实际出站类型。
func ResolveAutoByInboundType(inboundType inbound.InboundType) (OutboundType, error) {
	switch inboundType {
	case inbound.InboundTypeOpenAIChat:
		return OutboundTypeOpenAIChat, nil
	case inbound.InboundTypeOpenAIResponse:
		return OutboundTypeOpenAIResponse, nil
	case inbound.InboundTypeAnthropic:
		return OutboundTypeAnthropic, nil
	case inbound.InboundTypeOpenAIEmbedding:
		return OutboundTypeOpenAIEmbedding, nil
	default:
		return 0, fmt.Errorf("unsupported inbound type for auto channel: %d", inboundType)
	}
}
