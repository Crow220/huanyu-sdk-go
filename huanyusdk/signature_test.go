package huanyusdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// ---------- 向量驱动测试（向量即真理：common/vectors 由后端参考实现实测生成） ----------

type vectorCase struct {
	ID                string          `json:"id"`
	Params            json.RawMessage `json:"params"`
	ExpectedSignature string          `json:"expected_signature"`
}

type vectorFile struct {
	APISecret string       `json:"api_secret"`
	Cases     []vectorCase `json:"cases"`
}

func TestSignSignatureVectors(t *testing.T) {
	runVectorFile(t, "../common/vectors/signature_vectors.json")
}

func TestSignCallbackVectors(t *testing.T) {
	runVectorFile(t, "../common/vectors/callback_vectors.json")
}

// runVectorFile 逐用例跑 Sign，与向量期望签名比对。
func runVectorFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取向量文件失败: %v", err)
	}
	var vf vectorFile
	if err := json.Unmarshal(data, &vf); err != nil {
		t.Fatalf("解析向量文件失败: %v", err)
	}
	for _, c := range vf.Cases {
		t.Run(c.ID, func(t *testing.T) {
			params, err := decodeParams(c.Params)
			if err != nil {
				t.Fatalf("解析用例参数失败: %v", err)
			}
			got := Sign(params, vf.APISecret)
			if got != c.ExpectedSignature {
				t.Errorf("签名与向量不一致\n参数: %s\n期望: %s\n实际: %s", c.Params, c.ExpectedSignature, got)
			}
		})
	}
}

// decodeParams 把向量中的 params 对象解码为 Sign 的入参。
// 顶层先解为 map[string]json.RawMessage；形如 {...} 的值（如 payment_method）
// 必须经 OrderedPairs.UnmarshalJSON 保序解码——绝不能落到 map[string]interface{}（丢序）。
// null → nil；标量解为字符串（向量中标量值全是 JSON 字符串，如 "100.00"，规避浮点差异）。
func decodeParams(raw []byte) (map[string]interface{}, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("顶层参数解析失败: %w", err)
	}
	params := make(map[string]interface{}, len(top))
	for key, rawValue := range top {
		trimmed := bytes.TrimSpace(rawValue)
		switch {
		case string(trimmed) == "null":
			params[key] = nil
		case trimmed[0] == '{':
			pairs := NewOrderedPairs()
			if err := pairs.UnmarshalJSON(rawValue); err != nil {
				return nil, fmt.Errorf("字段 %q 解码失败: %w", key, err)
			}
			params[key] = pairs
		default:
			var s string
			if err := json.Unmarshal(trimmed, &s); err == nil {
				params[key] = s
				continue
			}
			// 容错：数字按字面转字符串，保住 "100.50" 的尾零形态
			var n json.Number
			if err := json.Unmarshal(trimmed, &n); err == nil {
				params[key] = n.String()
				continue
			}
			return nil, fmt.Errorf("字段 %q 不是字符串/数字/null", key)
		}
	}
	return params, nil
}

// ---------- Sign 行为单测 ----------

// 用户手工构造路径（Set 链式）必须与向量解码路径产出同一签名：
// 复用 array-payment-method-unsorted-keys 向量的期望值锁定。
func TestSignWithManuallyBuiltOrderedPairs(t *testing.T) {
	paymentMethod := NewOrderedPairs().
		Set("real_name", "李四").
		Set("card_number", "6217000000001234567").
		Set("bank", "中国银行").
		Set("sub_bank", "深圳分行")
	params := map[string]interface{}{
		"api_key":        "mk_test_001",
		"timestamp":      "1756684800",
		"nonce":          "abcdefgh12345678",
		"order_type":     "2",
		"payment_amount": "500.50",
		"payment_method": paymentMethod,
	}
	const want = "86079594AFB2DF774DD6CA83ABECE3DD"
	if got := Sign(params, "test-secret-0001"); got != want {
		t.Errorf("手工构造路径签名不匹配：期望 %s，实际 %s", want, got)
	}
}

// Sign 不得修改调用方传入的 map（第 1 步只跳过 signature，不做删除）。
func TestSignDoesNotMutateParams(t *testing.T) {
	params := map[string]interface{}{
		"api_key":    "mk_test_001",
		"order_type": "1",
		"signature":  "DEADBEEFShouldBeIgnored",
	}
	Sign(params, "sk")
	if _, ok := params["signature"]; !ok {
		t.Error("Sign 不应删除调用方 map 里的 signature 键")
	}
}

// 不支持的类型 fail fast：静默用 %v 拼串只会换来服务端验签失败，难以排查。
func TestSignPanicsOnUnsupportedParamType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("传入 float64 应触发 panic")
		}
	}()
	_ = Sign(map[string]interface{}{"payment_amount": 100.5}, "sk")
}

// typed-nil 的 *OrderedPairs 几乎必为未初始化变量，须明确报错而非静默当作空对象参与签名。
func TestSignPanicsOnTypedNilOrderedPairs(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("传入 (*OrderedPairs)(nil) 应触发 panic")
		}
	}()
	var paymentMethod *OrderedPairs // 声明后未初始化
	_ = Sign(map[string]interface{}{"payment_method": paymentMethod}, "sk")
}

// 全部参数被跳过的边界（spec 标注为不可达）：行为须与 PHP 参考实现一致，
// 即待签串为空串时仍拼 "&api_secret=..."（rtrim(”) = ”）。
// 期望值由 php -r 'echo strtoupper(md5("&api_secret=s"));' 实测得出。
func TestSignAllParamsSkippedMatchesPHP(t *testing.T) {
	params := map[string]interface{}{
		"remark": "",
		"extra":  nil,
	}
	const want = "11C0437C959E8E0EE50B80292663D07A"
	if got := Sign(params, "s"); got != want {
		t.Errorf("全跳过边界不匹配 PHP：期望 %s，实际 %s", want, got)
	}
}

// ---------- OrderedPairs 单元测试 ----------

func TestOrderedPairsSetOverwritesWithoutDuplicating(t *testing.T) {
	pairs := NewOrderedPairs().
		Set("bank", "中国银行").
		Set("sub_bank", "深圳分行").
		Set("bank", "工商银行")

	gotKeys := pairs.Keys()
	wantKeys := []string{"bank", "sub_bank"}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("键数不匹配：期望 %v，实际 %v", wantKeys, gotKeys)
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("键序不匹配：期望 %v，实际 %v", wantKeys, gotKeys)
		}
	}
	// 直接调 MarshalJSON：json.Marshal 会对 MarshalJSON 的输出再做 HTML 转义（<>& → \u003c 形式）
	got, err := pairs.MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"bank":"工商银行","sub_bank":"深圳分行"}`
	if string(got) != want {
		t.Errorf("覆盖后序列化不匹配：期望 %s，实际 %s", want, got)
	}
}

// 转义集逐字符锁定（与 php json_encode(..., JSON_UNESCAPED_UNICODE) 实测对齐）。
func TestOrderedPairsMarshalJSONEscapeRules(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"正斜杠转义", "k", `a/b`, `{"k":"a\/b"}`},
		{"键中的正斜杠也转义", "a/b", `1`, `{"a\/b":"1"}`},
		{"双引号转义", "k", `a"b`, `{"k":"a\"b"}`},
		{"反斜杠转义", "k", `a\b`, `{"k":"a\\b"}`},
		{"中文不转义", "k", "中国银行", `{"k":"中国银行"}`},
		{"尖括号与&不做HTML转义", "k", "<a>&b", `{"k":"<a>&b"}`},
		{"换行短转义", "k", "a\nb", `{"k":"a\nb"}`},
		{"回车短转义", "k", "a\rb", `{"k":"a\rb"}`},
		{"制表符短转义", "k", "a\tb", `{"k":"a\tb"}`},
		{"退格短转义", "k", "a\bb", `{"k":"a\bb"}`},
		{"换页短转义", "k", "a\fb", `{"k":"a\fb"}`},
		{"其余控制字符小写十六进制", "k", "a\x00\x01\x1fb", `{"k":"a\u0000\u0001\u001fb"}`},
		{"U+2028行分隔符转义（值）", "k", "深圳\u2028分行", `{"k":"深圳\u2028分行"}`},
		{"U+2029段分隔符转义（键）", "sub\u2029bank", "1", `{"sub\u2029bank":"1"}`},
		{"其余E2开头序列不受影响", "k", "破折号—省略号…", `{"k":"破折号—省略号…"}`},
		{"空字符串", "k", "", `{"k":""}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pairs := NewOrderedPairs().Set(c.key, c.value)
			got, err := pairs.MarshalJSON()
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			if string(got) != c.want {
				t.Errorf("转义不匹配：期望 %s，实际 %s", c.want, got)
			}
		})
	}
}

func TestOrderedPairsMarshalEmptyAsEmptyObject(t *testing.T) {
	got, err := NewOrderedPairs().MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if want := `{}`; string(got) != want {
		t.Errorf("空集序列化不匹配：期望 %s，实际 %s", want, got)
	}
}

func TestOrderedPairsUnmarshalPreservesKeyOrder(t *testing.T) {
	// 键序故意与字母序不同，防止实现偷偷按键排序蒙混过关
	const raw = `{"real_name":"李四","card_number":"6217000000001234567","bank":"中国银行","sub_bank":"深圳分行"}`
	pairs := NewOrderedPairs()
	if err := pairs.UnmarshalJSON([]byte(raw)); err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	wantKeys := []string{"real_name", "card_number", "bank", "sub_bank"}
	gotKeys := pairs.Keys()
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("键数不匹配：期望 %v，实际 %v", wantKeys, gotKeys)
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("键序不匹配：期望 %v，实际 %v", wantKeys, gotKeys)
		}
	}
	// round-trip：再序列化应得到无空格紧凑形态，且顺序不变
	got, err := pairs.MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"real_name":"李四","card_number":"6217000000001234567","bank":"中国银行","sub_bank":"深圳分行"}`
	if string(got) != want {
		t.Errorf("round-trip 不匹配：期望 %s，实际 %s", want, got)
	}
}

func TestOrderedPairsUnmarshalDuplicateKeyLastWins(t *testing.T) {
	pairs := NewOrderedPairs()
	if err := pairs.UnmarshalJSON([]byte(`{"a":"1","b":"2","a":"3"}`)); err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	got, err := pairs.MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if want := `{"a":"3","b":"2"}`; string(got) != want {
		t.Errorf("重复键应后者覆盖且只出现一次：期望 %s，实际 %s", want, got)
	}
}

func TestOrderedPairsUnmarshalRejectsInvalidInput(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"非对象", `["a","b"]`},
		{"标量", `"abc"`},
		{"嵌套对象值", `{"a":{"b":"1"}}`},
		{"数组值", `{"a":["1"]}`},
		{"null值", `{"a":null}`},
		{"对象后多余数据", `{"a":"1"} 1`},
		{"格式错误", `{"a"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := NewOrderedPairs().UnmarshalJSON([]byte(c.raw)); err == nil {
				t.Errorf("期望返回错误，实际通过")
			}
		})
	}
}
