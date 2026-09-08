package agentruntime

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DecodeCheckpointState 统一严格解码；诊断只保留类别、偏移和指纹，不回显会话内容。
// 版本、状态与运行记录的一致性仍由持有数据库事实的调用方校验。
func DecodeCheckpointState(raw string) (RuntimeState, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var state RuntimeState
	if err := decoder.Decode(&state); err != nil {
		reason, offset := "decode_error", decoder.InputOffset()
		var syntax *json.SyntaxError
		var mismatch *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntax):
			reason, offset = "syntax", syntax.Offset
		case errors.As(err, &mismatch):
			reason, offset = "type_mismatch", mismatch.Offset
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			reason = "empty_or_truncated"
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			// encoding/json 未导出未知字段错误类型；仅识别协议错误，不解释业务语义。
			reason = "unknown_field"
		}
		return RuntimeState{}, checkpointDecodeError(raw, reason, offset)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return RuntimeState{}, checkpointDecodeError(raw, "trailing_data", decoder.InputOffset())
	}
	return state, nil
}

func checkpointDecodeError(raw, reason string, offset int64) error {
	fingerprint := sha256.Sum256([]byte(raw))
	return fmt.Errorf("agent checkpoint state is invalid: reason=%s offset=%d fingerprint=%x", reason, offset, fingerprint[:8])
}
