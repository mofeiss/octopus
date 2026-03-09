package model

type GroupMode int

const (
	GroupModeRoundRobin GroupMode = 1 // 轮询：依次循环选择渠道
	GroupModeRandom     GroupMode = 2 // 随机：每次随机选择一个渠道
	GroupModeFailover   GroupMode = 3 // 故障转移：按优先级选择，失败时降级到下一个
	GroupModeWeighted   GroupMode = 4 // 加权分配：按优权重分配流量
)

type Group struct {
	ID                int         `json:"id" gorm:"primaryKey"`
	Name              string      `json:"name" gorm:"unique;not null"`
	Mode              GroupMode   `json:"mode" gorm:"not null"`
	MatchRegex        string      `json:"match_regex"`
	FirstTokenTimeOut int         `json:"first_token_time_out"` // 单个渠道首个Token响应超时时间(秒)
	SessionKeepTime   int         `json:"session_keep_time"`    // 会话保持时间(秒) 0 为禁用
	RouteAliases      string      `json:"route_aliases"`        // [fork] 逗号分隔的路由别名
	Remark            string      `json:"remark"`               // [fork] 分组备注
	SortOrder         int         `json:"sort_order"`           // [fork] 分组排序字段
	AutoHealthCheckEnabled         bool        `json:"auto_health_check_enabled" gorm:"default:false"`          // [fork] 自动测活开关
	AutoHealthCheckIntervalMinutes int         `json:"auto_health_check_interval_minutes" gorm:"default:0"`    // [fork] 自动测活间隔（分钟）
	AutoHealthCheckFailThreshold   int         `json:"auto_health_check_fail_threshold" gorm:"default:0"`      // [fork] 连续失败多少次自动禁用
	AutoHealthCheckNextRunAt       int64       `json:"auto_health_check_next_run_at" gorm:"default:0;index"`   // [fork] 自动测活下一次执行时间
	Items             []GroupItem `json:"items,omitempty" gorm:"foreignKey:GroupID"`
}

type GroupItem struct {
	ID        int    `json:"id" gorm:"primaryKey"`
	GroupID   int    `json:"group_id" gorm:"not null;index:idx_group_channel_model,unique"` // 创建时不携带此字段,更新时需要
	ChannelID int    `json:"channel_id" gorm:"not null;index:idx_group_channel_model,unique"`
	ModelName string `json:"model_name" gorm:"not null;index:idx_group_channel_model,unique"`
	Priority  int    `json:"priority"`
	Weight    int    `json:"weight"`
	Enabled   bool   `json:"enabled" gorm:"default:true"` // [fork] item 级启用开关
	HealthCheckTaskID              int64                       `json:"health_check_task_id,omitempty" gorm:"-"`
	HealthCheckStatus              GroupChannelCheckItemStatus `json:"health_check_status,omitempty" gorm:"-"`
	HealthCheckCheckedAt           int64                       `json:"health_check_checked_at,omitempty" gorm:"-"`
	HealthCheckConsecutiveFailures int                         `json:"health_check_consecutive_failures,omitempty" gorm:"-"`
	HealthCheckResponseStatusCode  int                         `json:"health_check_response_status_code,omitempty" gorm:"-"`
	HealthCheckDurationMs          int                         `json:"health_check_duration_ms,omitempty" gorm:"-"`
	HealthCheckError               string                      `json:"health_check_error,omitempty" gorm:"-"`
}

// GroupUpdateRequest 分组更新请求 - 仅包含变更的数据
type GroupUpdateRequest struct {
	ID                int                      `json:"id" binding:"required"`
	Name              *string                  `json:"name,omitempty"`                 // 仅在名称变更时发送
	Mode              *GroupMode               `json:"mode,omitempty"`                 // 仅在模式变更时发送
	MatchRegex        *string                  `json:"match_regex,omitempty"`          // 仅在匹配正则变更时发送
	FirstTokenTimeOut *int                     `json:"first_token_time_out,omitempty"` // 仅在超时变更时发送(秒)
	SessionKeepTime   *int                     `json:"session_keep_time,omitempty"`    // 仅在会话保持时间变更时发送(秒)
	RouteAliases      *string                  `json:"route_aliases,omitempty"`        // [fork] 仅在路由别名变更时发送
	Remark            *string                  `json:"remark,omitempty"`               // [fork] 仅在备注变更时发送
	AutoHealthCheckEnabled         *bool       `json:"auto_health_check_enabled,omitempty"`          // [fork] 自动测活开关
	AutoHealthCheckIntervalMinutes *int        `json:"auto_health_check_interval_minutes,omitempty"` // [fork] 自动测活间隔（分钟）
	AutoHealthCheckFailThreshold   *int        `json:"auto_health_check_fail_threshold,omitempty"`   // [fork] 连续失败阈值
	AutoHealthCheckNextRunAt       *int64      `json:"auto_health_check_next_run_at,omitempty"`      // [fork] 自动测活下一次执行时间
	ItemsToAdd        []GroupItemAddRequest    `json:"items_to_add,omitempty"`         // 新增的 items
	ItemsToUpdate     []GroupItemUpdateRequest `json:"items_to_update,omitempty"`      // 更新的 items (priority 变更)
	ItemsToDelete     []int                    `json:"items_to_delete,omitempty"`      // 删除的 item IDs
}

// GroupItemAddRequest 新增 item 请求
type GroupItemAddRequest struct {
	ChannelID int    `json:"channel_id" binding:"required"`
	ModelName string `json:"model_name" binding:"required"`
	Priority  int    `json:"priority,omitempty"`
	Weight    int    `json:"weight,omitempty"`
}

// GroupItemUpdateRequest 更新 item 请求
type GroupItemUpdateRequest struct {
	ID       int `json:"id" binding:"required"`
	Priority int `json:"priority,omitempty"`
	Weight   int `json:"weight,omitempty"`
}
type GroupIDAndLLMName struct {
	ChannelID int
	ModelName string
}

// [fork] 分组排序请求
type GroupReorderRequest struct {
	Orders []GroupOrderItem `json:"orders" binding:"required"`
}

type GroupOrderItem struct {
	ID        int `json:"id" binding:"required"`
	SortOrder int `json:"sort_order"`
}
