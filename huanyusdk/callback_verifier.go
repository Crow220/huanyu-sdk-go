package huanyusdk

import (
	"crypto/subtle"
	"net/url"
)

// CallbackVerifier 商户回调验签。入参为 application/x-www-form-urlencoded body
// 解析出的 url.Values（含 signature；net/http 侧 r.ParseForm + r.PostForm 即得）。
// 验签通过后商户应输出含 "success" 的 HTTP 200 响应，否则平台按 5/30/120/600s 重试共 5 次。
type CallbackVerifier struct {
	apiSecret string
}

// NewCallbackVerifier 创建回调验签器。
func NewCallbackVerifier(apiSecret string) *CallbackVerifier {
	return &CallbackVerifier{apiSecret: apiSecret}
}

// Verify 校验回调表单签名：任一键出现重复（一键多值）直接拒绝——PHP 表单解析取末值而
// Go url.Values.Get 取首值，重复键在两端语义分歧，一律拒绝更安全；signature 缺失或空串
// 直接拒绝（对齐 PHP empty() 判断）；其余字段按签名算法重算后与上行 signature
// 恒时比较（对齐 PHP hash_equals，防时序侧信道）。
func (v *CallbackVerifier) Verify(form url.Values) bool {
	for key := range form {
		if len(form[key]) > 1 {
			return false
		}
	}
	signature := form.Get("signature")
	if signature == "" {
		return false
	}
	expected := Sign(formToMap(form), v.apiSecret)
	// Go string 即 UTF-8 字节，直接比较字节序列
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

// formToMap 把回调表单转为 Sign 的入参形态；回调字段全标量（重复键已被 Verify 前置拒绝）。
func formToMap(form url.Values) map[string]interface{} {
	params := make(map[string]interface{}, len(form))
	for key := range form {
		params[key] = form.Get(key)
	}
	return params
}
