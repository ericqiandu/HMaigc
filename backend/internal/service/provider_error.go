package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

const contentModerationErrorCode = "sensitive_words_detected"

const contentModerationRetryMessage = "内容审核未通过，请修改提示词后重新生成；原任务不能直接重试"

const maxProviderFailureBodyBytes = 64 << 10

// 只提取供应商明确返回的错误码和短消息，避免把完整响应或用户输入复制到调用日志。
func providerFailureDetails(payload map[string]any) (string, string) {
	if baseResponse, ok := payload["base_resp"].(map[string]any); ok {
		code := normalizedProviderErrorCode(baseResponse["status_code"])
		if code != "" {
			return code, truncateRunes(strings.TrimSpace(stringField(baseResponse, "status_msg")), 500)
		}
	}
	candidates := make([]map[string]any, 0, 3)
	for _, key := range []string{"error", "data"} {
		if nested, ok := payload[key].(map[string]any); ok {
			candidates = append(candidates, nested)
		}
	}
	// 内层通常是供应商业务错误，外层 code 可能只是 HTTP 包装码。
	candidates = append(candidates, payload)
	code := ""
	message := ""
	for _, candidate := range candidates {
		if code == "" {
			code = normalizedProviderErrorCode(candidate["code"])
		}
		if message == "" {
			message = strings.TrimSpace(stringField(candidate, "message"))
		}
	}
	return code, truncateRunes(message, 500)
}

// providerHTTPFailureDetails 只读取供应商声明的短错误码与消息，不保留原始错误响应。
func providerHTTPFailureDetails(body string, secrets ...string) (string, string) {
	data := []byte(strings.TrimSpace(body))
	if len(data) == 0 || len(data) > maxProviderFailureBodyBytes || !json.Valid(data) {
		return "", ""
	}
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil || payload == nil {
		return "", ""
	}
	code, message := providerFailureDetails(payload)
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret != "" {
			code = strings.ReplaceAll(code, secret, "[REDACTED]")
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	return truncateRunes(code, 80), truncateRunes(message, 500)
}

func normalizedProviderErrorCode(value any) string {
	var code string
	switch current := value.(type) {
	case string:
		code = current
	case fmt.Stringer:
		code = current.String()
	case float64:
		if current != 0 {
			code = fmt.Sprintf("%g", current)
		}
	case int:
		if current != 0 {
			code = fmt.Sprintf("%d", current)
		}
	case int64:
		if current != 0 {
			code = fmt.Sprintf("%d", current)
		}
	}
	code = strings.TrimSpace(code)
	if code == "0" {
		return ""
	}
	return truncateRunes(code, 80)
}

func isContentModerationFailure(value string) bool {
	return strings.Contains(strings.ToLower(value), contentModerationErrorCode)
}
