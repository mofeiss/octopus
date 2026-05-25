package model

type GroupChannelCheckTaskStatus string

const (
	GroupChannelCheckTaskStatusPending        GroupChannelCheckTaskStatus = "pending"
	GroupChannelCheckTaskStatusRunning        GroupChannelCheckTaskStatus = "running"
	GroupChannelCheckTaskStatusSuccess        GroupChannelCheckTaskStatus = "success"
	GroupChannelCheckTaskStatusPartialSuccess GroupChannelCheckTaskStatus = "partial_success"
	GroupChannelCheckTaskStatusFailed         GroupChannelCheckTaskStatus = "failed"
)

type GroupChannelCheckTaskMode string

const (
	GroupChannelCheckTaskModeBatch  GroupChannelCheckTaskMode = "batch"
	GroupChannelCheckTaskModeSingle GroupChannelCheckTaskMode = "single"
)

type GroupChannelCheckItemStatus string

const (
	GroupChannelCheckItemStatusQueued  GroupChannelCheckItemStatus = "queued"
	GroupChannelCheckItemStatusPending GroupChannelCheckItemStatus = "pending"
	GroupChannelCheckItemStatusRunning GroupChannelCheckItemStatus = "running"
	GroupChannelCheckItemStatusSuccess GroupChannelCheckItemStatus = "success"
	GroupChannelCheckItemStatusFailed  GroupChannelCheckItemStatus = "failed"
)

// [fork] 手动测活发送协议。创建任务只加入队列，点击发送时才写入该协议并执行。
type GroupChannelCheckProtocol string

const (
	GroupChannelCheckProtocolOpenAIChat     GroupChannelCheckProtocol = "openai_chat"
	GroupChannelCheckProtocolOpenAIResponse GroupChannelCheckProtocol = "openai_response"
	GroupChannelCheckProtocolAnthropic      GroupChannelCheckProtocol = "anthropic"
)

func IsGroupChannelCheckProtocol(value string) bool {
	switch GroupChannelCheckProtocol(value) {
	case GroupChannelCheckProtocolOpenAIChat, GroupChannelCheckProtocolOpenAIResponse, GroupChannelCheckProtocolAnthropic:
		return true
	default:
		return false
	}
}

type GroupChannelCheckTask struct {
	ID           int64                       `json:"id" gorm:"primaryKey;autoIncrement:false"` // [fork] Snowflake ID
	GroupID      int                         `json:"group_id" gorm:"index"`
	GroupName    string                      `json:"group_name"`
	Mode         GroupChannelCheckTaskMode   `json:"mode"`
	Status       GroupChannelCheckTaskStatus `json:"status" gorm:"index"`
	TotalCount   int                         `json:"total_count"`
	PendingCount int                         `json:"pending_count"`
	RunningCount int                         `json:"running_count"`
	SuccessCount int                         `json:"success_count"`
	FailedCount  int                         `json:"failed_count"`
	CreatedAt    int64                       `json:"created_at" gorm:"index"`
	StartedAt    int64                       `json:"started_at"`
	FinishedAt   int64                       `json:"finished_at"`
	LastError    string                      `json:"last_error"`
	Items        []GroupChannelCheckTaskItem `json:"items,omitempty" gorm:"foreignKey:TaskID"`
}

type GroupChannelCheckSyncResult struct {
	GroupID       int    `json:"group_id"`
	GroupName     string `json:"group_name"`
	EnabledCount  int    `json:"enabled_count"`
	DisabledCount int    `json:"disabled_count"`
	SkippedCount  int    `json:"skipped_count"`
}

type GroupChannelCheckTaskItem struct {
	ID                   int64                       `json:"id" gorm:"primaryKey;autoIncrement:false"` // [fork] Snowflake ID
	TaskID               int64                       `json:"task_id" gorm:"index"`
	GroupID              int                         `json:"group_id" gorm:"index"`
	GroupItemID          int                         `json:"group_item_id,omitempty" gorm:"default:0"`
	ChannelID            int                         `json:"channel_id" gorm:"index"`
	ChannelName          string                      `json:"channel_name"`
	ChannelType          int                         `json:"channel_type"`
	ModelName            string                      `json:"model_name"`
	Status               GroupChannelCheckItemStatus `json:"status" gorm:"index"`
	RequestKind          string                      `json:"request_kind"`
	RequestURL           string                      `json:"request_url"`
	BaseURL              string                      `json:"base_url"`
	ChannelKeyID         int                         `json:"channel_key_id,omitempty" gorm:"default:0"`
	ChannelKeyIndex      int                         `json:"channel_key_index,omitempty" gorm:"default:0"`
	ChannelKeyPreview    string                      `json:"channel_key_preview,omitempty"`
	ChannelKeyRemark     string                      `json:"channel_key_remark,omitempty"`
	ResponseStatusCode   int                         `json:"response_status_code"`
	DurationMs           int                         `json:"duration_ms"`
	RequestContent       string                      `json:"request_content"`
	OpenAIRequestCurl    string                      `json:"openai_request_curl" gorm:"column:openai_request_curl"`       // [fork] OpenAI 兼容 curl
	AnthropicRequestCurl string                      `json:"anthropic_request_curl" gorm:"column:anthropic_request_curl"` // [fork] Anthropic 兼容 curl
	ResponsePreview      string                      `json:"response_preview"`
	ResponseContent      string                      `json:"response_content"`
	Error                string                      `json:"error"`
	StartedAt            int64                       `json:"started_at"`
	FinishedAt           int64                       `json:"finished_at"`
	Attempts             []GroupChannelCheckAttempt  `json:"attempts,omitempty" gorm:"serializer:json"` // [fork] 多 key 测活尝试明细
}

type GroupChannelCheckAttempt struct {
	Status               GroupChannelCheckItemStatus `json:"status"`
	ChannelKeyID         int                         `json:"channel_key_id,omitempty"`
	ChannelKeyIndex      int                         `json:"channel_key_index,omitempty"`
	ChannelKeyPreview    string                      `json:"channel_key_preview,omitempty"`
	ChannelKeyRemark     string                      `json:"channel_key_remark,omitempty"`
	ResponseStatusCode   int                         `json:"response_status_code"`
	OpenAIRequestCurl    string                      `json:"openai_request_curl,omitempty"`    // [fork] 当前 key 的 OpenAI 兼容 curl
	AnthropicRequestCurl string                      `json:"anthropic_request_curl,omitempty"` // [fork] 当前 key 的 Anthropic 兼容 curl
	ResponseContent      string                      `json:"response_content"`
	Error                string                      `json:"error"`
}

type GroupChannelCheckCreateRequest struct {
	GroupID     int    `json:"group_id" binding:"required"`
	GroupItemID int    `json:"group_item_id,omitempty"`
	ChannelID   int    `json:"channel_id,omitempty"`
	ModelName   string `json:"model_name,omitempty"`
}

type GroupChannelCheckSendRequest struct {
	TaskID   int64                     `json:"task_id" binding:"required"`
	ItemID   int64                     `json:"item_id,omitempty"`
	Protocol GroupChannelCheckProtocol `json:"protocol" binding:"required"`
}

type GroupChannelCheckState struct {
	ID                  int64                       `json:"id" gorm:"primaryKey;autoIncrement:false"` // [fork] Snowflake ID
	GroupID             int                         `json:"group_id" gorm:"index"`
	GroupItemID         int                         `json:"group_item_id" gorm:"uniqueIndex"`
	ChannelID           int                         `json:"channel_id" gorm:"index"`
	ModelName           string                      `json:"model_name"`
	TaskID              int64                       `json:"task_id" gorm:"index"`
	Status              GroupChannelCheckItemStatus `json:"status" gorm:"index"`
	CheckedAt           int64                       `json:"checked_at" gorm:"index"`
	SuccessAt           int64                       `json:"success_at"`
	FailedAt            int64                       `json:"failed_at"`
	ConsecutiveFailures int                         `json:"consecutive_failures"`
	ResponseStatusCode  int                         `json:"response_status_code"`
	DurationMs          int                         `json:"duration_ms"`
	Error               string                      `json:"error"`
}
