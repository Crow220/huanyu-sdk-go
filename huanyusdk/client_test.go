package huanyusdk

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Client 全链路测试（httptest.NewServer）：参数注入、括号记法展平、
// 服务端视角重算签名、信封解析（含 pending_identity 分支、code!=1、非 2xx/非 JSON 防御）。

const (
	testAPIKey    = "mk_test_001"
	testAPISecret = "test-secret-0001"
)

// startTestServer 起一个 httptest 服务并返回指向它的 Client。
// baseURL 故意带尾部 "/"，顺带锁定 WithBaseURL 的去尾斜杠行为。
func startTestServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(testAPIKey, testAPISecret, WithBaseURL(server.URL+"/addons/huanyu/"))
}

// kvPair 到达序视角的单个表单键值对（url.Values 是无序 map，还原不了 PHP parse_str 的到达序）。
type kvPair struct {
	key   string
	value string
}

// parseOrderedForm 按到达序解析 application/x-www-form-urlencoded body，模拟服务端收到形态。
func parseOrderedForm(t *testing.T, body string) []kvPair {
	t.Helper()
	var out []kvPair
	if body == "" {
		return out
	}
	for _, part := range strings.Split(body, "&") {
		if part == "" {
			continue
		}
		rawKey, rawValue, _ := strings.Cut(part, "=")
		key, err := url.QueryUnescape(rawKey)
		if err != nil {
			t.Fatalf("form 键 %q 解码失败: %v", rawKey, err)
		}
		value, err := url.QueryUnescape(rawValue)
		if err != nil {
			t.Fatalf("form 值 %q 解码失败: %v", rawValue, err)
		}
		out = append(out, kvPair{key: key, value: value})
	}
	return out
}

// flatToMap 把扁平键值对转为 map（一键一值场景）。
func flatToMap(flat []kvPair) map[string]string {
	out := make(map[string]string, len(flat))
	for _, pair := range flat {
		out[pair.key] = pair.value
	}
	return out
}

// indexOfKey 返回键在到达序中的位置，不存在返回 -1。
func indexOfKey(flat []kvPair, key string) int {
	for i, pair := range flat {
		if pair.key == key {
			return i
		}
	}
	return -1
}

// renest 模拟服务端 PHP parse_str + json_encode 视角：按到达序把括号记法扁平键重嵌套，
// 嵌套层用 OrderedPairs 承载到达序（其 JSON 化键序即服务端 json_encode 键序）。
func renest(t *testing.T, flat []kvPair) map[string]interface{} {
	t.Helper()
	out := make(map[string]interface{})
	for _, pair := range flat {
		bracket := strings.Index(pair.key, "[")
		if bracket < 0 {
			out[pair.key] = pair.value
			continue
		}
		name := pair.key[:bracket]
		segment := strings.TrimSuffix(strings.TrimPrefix(pair.key[bracket:], "["), "]")
		if strings.Contains(segment, "[") {
			t.Fatalf("测试辅助仅支持一层嵌套键: %s", pair.key)
		}
		node, _ := out[name].(*OrderedPairs)
		if node == nil {
			node = NewOrderedPairs()
			out[name] = node
		}
		node.Set(segment, pair.value)
	}
	return out
}

// ① CreateOrder 含乱序 PaymentMethod：括号记法展平 + 服务端视角重嵌套重算签名比对。
func TestClientCreateOrderFlattensNestedPaymentMethod(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(body)
		fmt.Fprint(w, `{"code":1,"msg":"订单创建成功","time":1756684800,`+
			`"data":{"order_no":"HY002","result_status":"success"}}`)
	})

	// 乱序插入（非字母序）：real_name → bank → sub_branch → card_no
	paymentMethod := NewOrderedPairs().
		Set("real_name", "张三").
		Set("bank", "工商银行").
		Set("sub_branch", "杭州分行").
		Set("card_no", "6222020200112233445")
	order, err := client.CreateOrder(&CreateOrderParams{
		OrderType:       "2",
		CnyAmount:       "500.00",
		PaymentMethod:   paymentMethod,
		MerchantOrderNo: "M002",
	})
	if err != nil {
		t.Fatalf("CreateOrder 失败: %v", err)
	}
	if order["order_no"] != "HY002" || order["result_status"] != "success" {
		t.Errorf("data 解析不符：%v", order)
	}

	if gotMethod != http.MethodPost || gotPath != "/addons/huanyu/merchant/createOrder" {
		t.Errorf("请求行不符：%s %s", gotMethod, gotPath)
	}

	flat := parseOrderedForm(t, gotBody)
	sent := flatToMap(flat)
	for _, key := range []string{
		"payment_method[real_name]", "payment_method[bank]",
		"payment_method[sub_branch]", "payment_method[card_no]",
		"api_key", "timestamp", "nonce", "signature", "order_type", "cny_amount", "merchant_order_no",
	} {
		if _, ok := sent[key]; !ok {
			t.Errorf("form 缺少扁平键 %s", key)
		}
	}
	if sent["payment_method[bank]"] != "工商银行" {
		t.Errorf("payment_method[bank] 值不符：%q", sent["payment_method[bank]"])
	}
	for _, pair := range flat {
		if strings.HasPrefix(pair.value, "{") {
			t.Errorf("不允许 { 开头的 JSON 直传垃圾值：%s=%s", pair.key, pair.value)
		}
	}
	// 嵌套键序须保持插入序（乱序）：到达序位置递增——这正是服务端 json_encode 键序
	last := -1
	for _, key := range []string{
		"payment_method[real_name]", "payment_method[bank]",
		"payment_method[sub_branch]", "payment_method[card_no]",
	} {
		idx := indexOfKey(flat, key)
		if idx < 0 {
			t.Fatalf("缺少扁平键 %s", key)
		}
		if idx <= last {
			t.Errorf("嵌套键序未保持插入序：%s 到达过晚", key)
		}
		last = idx
	}
	// 通用参数形态
	if sent["api_key"] != testAPIKey {
		t.Errorf("api_key 不符：%q", sent["api_key"])
	}
	if !regexp.MustCompile(`^\d{10}$`).MatchString(sent["timestamp"]) {
		t.Errorf("timestamp 应为秒级数字串：%q", sent["timestamp"])
	}
	if !regexp.MustCompile(`^[0-9A-Za-z]{16}$`).MatchString(sent["nonce"]) {
		t.Errorf("nonce 应为 16 位字母数字：%q", sent["nonce"])
	}
	// 服务端视角：重嵌套（到达序）后重算签名，应与上行 signature 一致
	if got, want := Sign(renest(t, flat), testAPISecret), sent["signature"]; got != want {
		t.Errorf("重嵌套后重算签名不一致：期望 %s，实际 %s", want, got)
	}
}

// ② pending_identity 分支：data 含 result_status / identity_url。
func TestClientCreateOrderPendingIdentityReturnsUrl(t *testing.T) {
	client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":1,"msg":"需补充客户身份信息",`+
			`"data":{"result_status":"pending_identity","identity_url":"https://x.test/i?o=1"}}`)
	})

	order, err := client.CreateOrder(&CreateOrderParams{OrderType: "1", CnyAmount: "1.00"})
	if err != nil {
		t.Fatalf("CreateOrder 失败: %v", err)
	}
	if order["result_status"] != "pending_identity" {
		t.Errorf("result_status 不符：%v", order["result_status"])
	}
	if order["identity_url"] != "https://x.test/i?o=1" {
		t.Errorf("identity_url 不符：%v", order["identity_url"])
	}
}

// ③ code=0 返回 *ApiError，断言 Code/Msg/Time。
func TestClientNonSuccessCodeReturnsApiError(t *testing.T) {
	client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"msg":"签名错误","time":1756684800,"data":null}`)
	})

	data, err := client.OrderList(&OrderListFilters{Page: "1"})
	if err == nil {
		t.Fatalf("code=0 应返回错误，实际得到 data=%v", data)
	}
	apiErr, ok := err.(*ApiError)
	if !ok {
		t.Fatalf("应返回 *ApiError，实际 %T: %v", err, err)
	}
	if apiErr.Code != 0 {
		t.Errorf("Code 期望 0，实际 %d", apiErr.Code)
	}
	if apiErr.Msg != "签名错误" {
		t.Errorf("Msg 期望 签名错误，实际 %q", apiErr.Msg)
	}
	if apiErr.Time != 1756684800 {
		t.Errorf("Time 期望 1756684800，实际 %d", apiErr.Time)
	}
}

// ④ OrderDetail：GET query 含 order_no 与 signature，未知键被白名单过滤，服务端重算签名一致。
func TestClientOrderDetailSendsFilteredQuery(t *testing.T) {
	var gotMethod, gotPath string
	var gotQuery url.Values
	client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.Query()
		fmt.Fprint(w, `{"code":1,"msg":"获取成功","data":{"order_no":"HY001","status":"completed"}}`)
	})

	detail, err := client.OrderDetail(map[string]string{"order_no": "HY001", "hack": "evil"})
	if err != nil {
		t.Fatalf("OrderDetail 失败: %v", err)
	}
	if detail["order_no"] != "HY001" || detail["status"] != "completed" {
		t.Errorf("data 解析不符：%v", detail)
	}

	if gotMethod != http.MethodGet || gotPath != "/addons/huanyu/merchant/orderDetailApi" {
		t.Errorf("请求行不符：%s %s", gotMethod, gotPath)
	}
	if gotQuery.Get("order_no") != "HY001" {
		t.Errorf("query 应含 order_no=HY001，实际 %v", gotQuery)
	}
	if gotQuery.Get("signature") == "" {
		t.Error("query 应含 signature")
	}
	if _, ok := gotQuery["hack"]; ok {
		t.Error("未知键应被 OrderDetailQuery 白名单过滤")
	}
	// 服务端视角重算 GET query 签名（全标量，map 无序无碍）
	sent := make(map[string]interface{}, len(gotQuery))
	for key := range gotQuery {
		sent[key] = gotQuery.Get(key)
	}
	if got, want := Sign(sent, testAPISecret), gotQuery.Get("signature"); got != want {
		t.Errorf("GET 签名与服务端重算不一致：期望 %s，实际 %s", want, got)
	}
}

// ⑤ ConfirmPayment：paymentProof 空串不上行该键，非空原样上行；data null 返回空 map。
func TestClientConfirmPaymentOptionalProof(t *testing.T) {
	t.Run("非空凭证上行", func(t *testing.T) {
		client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"code":1,"msg":"付款确认成功","data":{}}`)
		})

		result, err := client.ConfirmPayment("HY001", "https://cdn.test/p.png")
		if err != nil {
			t.Fatalf("ConfirmPayment 失败: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("空 data 应返回空 map，实际 %v", result)
		}
	})

	t.Run("空凭证不上行", func(t *testing.T) {
		var gotBody string
		client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
			fmt.Fprint(w, `{"code":1,"msg":"付款确认成功","data":null}`)
		})

		result, err := client.ConfirmPayment("HY001", "")
		if err != nil {
			t.Fatalf("ConfirmPayment 失败: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("data null 应返回空 map，实际 %v", result)
		}
		sent := flatToMap(parseOrderedForm(t, gotBody))
		if sent["order_no"] != "HY001" {
			t.Errorf("order_no 不符：%q", sent["order_no"])
		}
		if _, ok := sent["payment_proof"]; ok {
			t.Error("paymentProof 为空串时不应上行 payment_proof")
		}
		if got, want := Sign(renest(t, parseOrderedForm(t, gotBody)), testAPISecret), sent["signature"]; got != want {
			t.Errorf("空凭证请求签名与服务端重算不一致：期望 %s，实际 %s", want, got)
		}
	})
}

// ⑥ UploadPaymentProof：POST 上行 order_no 与 proof_image_url。
func TestClientUploadPaymentProofSendsForm(t *testing.T) {
	var gotBody string
	client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		fmt.Fprint(w, `{"code":1,"msg":"上传成功","data":{}}`)
	})

	if _, err := client.UploadPaymentProof("HY001", "https://cdn.test/proof.png"); err != nil {
		t.Fatalf("UploadPaymentProof 失败: %v", err)
	}
	sent := flatToMap(parseOrderedForm(t, gotBody))
	if sent["order_no"] != "HY001" || sent["proof_image_url"] != "https://cdn.test/proof.png" {
		t.Errorf("form 参数不符：%v", sent)
	}
}

// ⑦ 嵌套空串回归：PaymentMethod 含空串叶子（sub_bank 未填）时必须上行
// payment_method[sub_bank]= 空值键——服务端 parse_str 重嵌套后 json_encode 才保得住该键，
// 与客户端签名时的 JSON 形态一致。修复前展平跳过空串 → 服务端重嵌套缺键 → 拒签。
func TestClientCreateOrderNestedEmptyStringLeafRoundtrip(t *testing.T) {
	var gotBody string
	client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		fmt.Fprint(w, `{"code":1,"msg":"订单创建成功","time":1756684800,`+
			`"data":{"order_no":"HY003","result_status":"success"}}`)
	})

	paymentMethod := NewOrderedPairs().
		Set("bank", "工商银行").
		Set("sub_bank", "").
		Set("card_number", "6222020200112233445").
		Set("real_name", "张三")
	if _, err := client.CreateOrder(&CreateOrderParams{
		OrderType:       "2",
		CnyAmount:       "500.00",
		PaymentMethod:   paymentMethod,
		MerchantOrderNo: "M003",
	}); err != nil {
		t.Fatalf("CreateOrder 失败: %v", err)
	}

	flat := parseOrderedForm(t, gotBody)
	sent := flatToMap(flat)
	value, ok := sent["payment_method[sub_bank]"]
	if !ok {
		t.Fatal("嵌套空串叶子必须上行：缺少 payment_method[sub_bank] 键")
	}
	if value != "" {
		t.Errorf("payment_method[sub_bank] 应为空值：%q", value)
	}
	// 服务端视角：重嵌套（含空串叶子）后重算签名，应与上行 signature 一致
	if got, want := Sign(renest(t, flat), testAPISecret), sent["signature"]; got != want {
		t.Errorf("含空串叶子的重嵌套重算签名不一致：期望 %s，实际 %s", want, got)
	}
}

// 信封防御：响应非 JSON 或缺 code 返回错误，消息含截断片段。
func TestClientMalformedResponseReturnsError(t *testing.T) {
	t.Run("非JSON", func(t *testing.T) {
		client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `<html>gateway error</html>`)
		})

		_, err := client.OrderList(&OrderListFilters{})
		if err == nil {
			t.Fatal("非 JSON 响应应返回错误")
		}
		if !strings.Contains(err.Error(), "平台响应格式异常") {
			t.Errorf("错误应说明响应格式异常：%v", err)
		}
		if !strings.Contains(err.Error(), "<html>gateway error") {
			t.Errorf("错误消息应含响应片段：%v", err)
		}
	})

	t.Run("缺code", func(t *testing.T) {
		client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"msg":"缺 code"}`)
		})

		_, err := client.OrderList(&OrderListFilters{})
		if err == nil {
			t.Fatal("缺 code 的响应应返回错误")
		}
		if !strings.Contains(err.Error(), "平台响应格式异常") || !strings.Contains(err.Error(), "缺 code") {
			t.Errorf("错误应含格式异常说明与响应片段：%v", err)
		}
	})
}

// 非 2xx：返回错误并含 HTTP 状态与截断 body（不走信封解析）。
func TestClientNon2xxResponseReturnsError(t *testing.T) {
	client := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	})

	_, err := client.OrderList(&OrderListFilters{})
	if err == nil {
		t.Fatal("HTTP 502 应返回错误")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "Bad Gateway") {
		t.Errorf("错误应含 HTTP 状态与响应片段：%v", err)
	}
}

// WithHTTPClient：注入的 http.Client 被使用；未配置 WithBaseURL 时走默认生产 baseURL；
// 顺带覆盖 nil filters（零值参数只有通用参数仍可发起请求）。
func TestClientWithHTTPClientAndDefaultBaseURL(t *testing.T) {
	var gotURL string
	stub := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResponse(`{"code":1,"msg":"ok","data":{}}`), nil
	})}
	client := NewClient(testAPIKey, testAPISecret, WithHTTPClient(stub))

	if _, err := client.OrderList(nil); err != nil {
		t.Fatalf("OrderList(nil) 失败: %v", err)
	}
	if !strings.HasPrefix(gotURL, DefaultBaseURL+"/merchant/orderListApi?") {
		t.Errorf("未配置 WithBaseURL 应走默认生产 baseURL：%s", gotURL)
	}
	if !strings.Contains(gotURL, "signature=") || !strings.Contains(gotURL, "api_key="+testAPIKey) {
		t.Errorf("GET query 应含通用参数：%s", gotURL)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// jsonResponse 构造信封 JSON 的 HTTP 响应（stub Transport 用）。
func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// flattenParams 直测：仅 nil 不上行；空串（含顶层）照常成对上行。
// 顶层空串上行后服务端重算签名同样跳过（等价），嵌套空串上行是保住
// json_encode 键值形态的硬要求（见 TestClientCreateOrderNestedEmptyStringLeafRoundtrip）。
func TestFlattenParamsSkipsNilOnly(t *testing.T) {
	flat := make(formPairs, 0)
	flattenParams("remark", "", &flat)
	flattenParams("extra", nil, &flat)
	flattenParams("order_type", "2", &flat)

	if len(flat) != 2 {
		t.Fatalf("应上行空串与普通值、仅跳过 nil：实际 %v", flat)
	}
	if flat[0].key != "remark" || flat[0].value != "" {
		t.Errorf("空串应原样成对上行：实际 %v", flat[0])
	}
	if flat[1].key != "order_type" || flat[1].value != "2" {
		t.Errorf("普通值成对不符：实际 %v", flat[1])
	}
}
