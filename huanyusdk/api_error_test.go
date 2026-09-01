package huanyusdk

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestApiErrorMessageContainsOverview(t *testing.T) {
	err := NewApiError(0, "签名错误", 1756684800)
	msg := err.Error()
	if !strings.Contains(msg, "签名错误") {
		t.Errorf("Error() 应以平台消息为主，实际 %q", msg)
	}
	if !strings.Contains(msg, "code=0") || !strings.Contains(msg, "time=1756684800") {
		t.Errorf("Error() 应含 Code/Time 概览，实际 %q", msg)
	}

	// errors.As 链路可用（调用方 wrap 后仍能取回业务码）
	var apiErr *ApiError
	if !errors.As(fmt.Errorf("下单失败: %w", err), &apiErr) {
		t.Fatal("errors.As 应能解出 *ApiError")
	}
	if apiErr.Code != 0 || apiErr.Msg != "签名错误" || apiErr.Time != 1756684800 {
		t.Errorf("字段不符：Code=%d Msg=%q Time=%d", apiErr.Code, apiErr.Msg, apiErr.Time)
	}
}
