package huanyusdk

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Sign 按 spec/signature.md 六步计算签名，行为由 common/vectors 测试向量锁定。
// params 的值只接受 string、*OrderedPairs（非 nil，数组类参数）与 nil（跳过）；
// 数字请以字符串形式传入（如 "100.50"），规避各语言浮点序列化差异。
func Sign(params map[string]interface{}, apiSecret string) string {
	// 第 1 步：移除 signature 字段本身（只跳过、不删除，不修改调用方的 map）。
	keys := make([]string, 0, len(params))
	for key := range params {
		if key == "signature" {
			continue
		}
		keys = append(keys, key)
	}

	// 第 3 步：顶层按键名升序。sort.Strings 是字节序，即 spec 要求的 ASCII 序
	//（可达参数域内与 PHP ksort 的 SORT_REGULAR 等价，见 spec 跨语言注意）。
	sort.Strings(keys)

	// 第 2/4 步：数组参数 JSON 化（保序序列化），跳过 null 与空字符串，拼 key=value&。
	var b strings.Builder
	for _, key := range keys {
		var value string
		switch v := params[key].(type) {
		case *OrderedPairs:
			if v == nil {
				// typed-nil 大概率是声明后未初始化的变量，静默参与签名只会换来服务端拒签
				panic(fmt.Sprintf("huanyusdk: 参数 %q 为 (*OrderedPairs)(nil)，请传 NewOrderedPairs() 构造的实例或 nil", key))
			}
			// 第 2 步：数组/对象参数序列化为与 PHP json_encode 对齐的字符串。
			encoded, err := v.MarshalJSON()
			if err != nil {
				// 手写序列化器实际不会返回错误，此处为防御
				panic(fmt.Sprintf("huanyusdk: 参数 %q 序列化失败: %v", key, err))
			}
			value = string(encoded)
		case string:
			value = v
		case nil:
			continue // 第 4 步：跳过 null
		default:
			// 类型契约 fail fast：错误类型参与签名只会换来服务端验签失败，难以排查
			panic(fmt.Sprintf("huanyusdk: 参数 %q 类型不受支持（%T），请传 string、*OrderedPairs 或 nil", key, v))
		}
		if value == "" {
			continue // 第 4 步：跳过空字符串
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(value)
		b.WriteByte('&')
	}
	stringToSign := strings.TrimSuffix(b.String(), "&") // 等价 PHP 的 rtrim($s, '&')

	// 第 5 步：追加 &api_secret=<SECRET>（不做 URL 编码）。
	stringToSign += "&api_secret=" + apiSecret

	// 第 6 步：MD5 转大写（Go string 即 UTF-8 字节，与 PHP md5() 一致）。
	sum := md5.Sum([]byte(stringToSign))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
