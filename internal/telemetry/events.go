package telemetry

// 事件 code 常量（对应官方 CLI 的 Events 枚举）
const (
	EventChatRequestSend      = "chat_request_send"
	EventChatMessageResponse  = "chat_message_response"
	EventCompletionTrigger    = "completion_trigger"
	EventCompletionResponse   = "completion_response"
	EventCompletionAction     = "completion_action"
	EventPageLoad             = "page_load"
	EventLogin                = "login"
	EventResponsesRequest     = "responses_request_send"
	EventResponsesResponse    = "responses_message_response"
	EventUpstreamRetry          = "upstream_retry"
	EventUpstreamFailure        = "upstream_failure"
)

// CompletionAction 值
const (
	ActionAccept = "accept"
	ActionReject = "reject"
)

// Event 表示一个待上报的事件
type Event struct {
	EventCode   string                 `json:"eventCode"`
	Timestamp   int64                  `json:"timestamp"`
	ReportDelay int64                  `json:"reportDelay"`
	Data        map[string]interface{} `json:"data,omitempty"`
}
