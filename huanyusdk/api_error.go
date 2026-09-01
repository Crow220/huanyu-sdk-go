package huanyusdk

import "fmt"

// ApiError 平台返回业务失败（响应信封 code != 1）时的错误，如签名错误、商户单号已存在等。
// Code 对应信封 code，Time 对应信封 time（信封缺失时为 0）。
type ApiError struct {
	Code int
	Msg  string
	Time int64
}

// NewApiError 构造业务失败错误。
func NewApiError(code int, msg string, time int64) *ApiError {
	return &ApiError{Code: code, Msg: msg, Time: time}
}

// Error 以平台消息为主，附 code/time 概览便于排障时对上服务端日志。
func (e *ApiError) Error() string {
	return fmt.Sprintf("%s（code=%d, time=%d）", e.Msg, e.Code, e.Time)
}
