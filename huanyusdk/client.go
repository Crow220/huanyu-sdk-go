package huanyusdk

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL 生产环境基础 URL（联调用 WithBaseURL 替换）。
const DefaultBaseURL = "https://api.pisces-pay.cn/addons/huanyu"

const (
	nonceLength = 16
	nonceChars  = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// Option Client 可选配置项。
type Option func(*Client)

// WithBaseURL 覆盖基础 URL，尾部 "/" 会被去除（对齐 PHP rtrim 语义）。
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient 覆盖 HTTP 客户端（默认 30s 超时）。
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// Client 商户 API 客户端。
// merchant_order_no 商户内唯一：同商户重复单号建单返回"商户单号已存在"错误，
// 超时后可凭同一单号安全重试（返回已存在即代表首单已建成）。
type Client struct {
	apiKey     string
	apiSecret  string
	baseURL    string
	httpClient *http.Client
}

// NewClient 创建客户端，默认指向生产 baseURL 与 30s 超时的 http.Client。
func NewClient(apiKey, apiSecret string, opts ...Option) *Client {
	client := &Client{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// CreateOrder 创建订单（POST /merchant/createOrder），参数经 CreateOrderParams 白名单与零值跳过。
func (c *Client) CreateOrder(params *CreateOrderParams) (map[string]interface{}, error) {
	return c.doRequest(http.MethodPost, "/merchant/createOrder", params.ToSignParams())
}

// OrderList 分页查询订单列表（GET /merchant/orderListApi）。
func (c *Client) OrderList(filters *OrderListFilters) (map[string]interface{}, error) {
	return c.doRequest(http.MethodGet, "/merchant/orderListApi", filters.ToSignParams())
}

// OrderDetail 查询订单详情（GET /merchant/orderDetailApi，id / order_no / merchant_order_no 三选一）。
func (c *Client) OrderDetail(query map[string]string) (map[string]interface{}, error) {
	return c.doRequest(http.MethodGet, "/merchant/orderDetailApi", OrderDetailQuery(query))
}

// UploadPaymentProof 上传支付凭证（POST /merchant/uploadPaymentProof）。
func (c *Client) UploadPaymentProof(orderNo, proofImageUrl string) (map[string]interface{}, error) {
	return c.doRequest(http.MethodPost, "/merchant/uploadPaymentProof", map[string]interface{}{
		"order_no":        orderNo,
		"proof_image_url": proofImageUrl,
	})
}

// ConfirmPayment 确认付款（POST /merchant/confirmPayment），paymentProof 为空串时不上行该键。
func (c *Client) ConfirmPayment(orderNo string, paymentProof string) (map[string]interface{}, error) {
	params := map[string]interface{}{"order_no": orderNo}
	if paymentProof != "" {
		params["payment_proof"] = paymentProof
	}
	return c.doRequest(http.MethodPost, "/merchant/confirmPayment", params)
}

// doRequest 统一请求管道：注入通用参数（api_key / timestamp / nonce / signature）→
// 括号记法展平 → GET 拼 query、POST 走 form → 解析响应信封 {code, msg, data, time}。
// 签名在原始嵌套参数上计算（Sign 内部对 *OrderedPairs JSON 化），展平只影响上行形态，
// 两条路径互不干扰——与后端"括号记法重嵌套→json_encode 重算"的验签路径对齐。
func (c *Client) doRequest(method, path string, params map[string]interface{}) (map[string]interface{}, error) {
	// 防御性拷贝：注入通用参数不影响调用方传入的 map
	signed := make(map[string]interface{}, len(params)+4)
	for key, value := range params {
		signed[key] = value
	}
	signed["api_key"] = c.apiKey
	signed["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10) // 秒级字符串，对齐 PHP time()
	signed["nonce"] = randomNonce()
	signed["signature"] = Sign(signed, c.apiSecret)

	encoded := flattenAll(signed).encode()

	requestURL := c.baseURL + path
	var body io.Reader
	if method == http.MethodGet {
		if encoded != "" {
			requestURL += "?" + encoded
		}
	} else {
		body = strings.NewReader(encoded)
	}
	request, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("寰宇接口请求构造失败: %s %s: %w", method, path, err)
	}
	if method != http.MethodGet {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("寰宇接口请求失败: %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("寰宇接口响应读取失败: %s %s: %w", method, path, err)
	}
	return parseEnvelope(response.StatusCode, responseBody)
}

// formPair 扁平化后的单个键值对。
type formPair struct {
	key   string
	value string
}

// formPairs 保序的扁平键值对列表。不用 url.Values 承载：它是无序 map 且 Encode() 按键
// 排序，会破坏嵌套键的插入序——而嵌套键序正是服务端验签的键序（见 flattenParams）。
type formPairs []formPair

// encode 输出 application/x-www-form-urlencoded 形态：
// 与 url.Values.Encode 同款 QueryEscape 转义，但不做键排序（保持到达序）。
func (fp formPairs) encode() string {
	parts := make([]string, 0, len(fp))
	for _, pair := range fp {
		parts = append(parts, url.QueryEscape(pair.key)+"="+url.QueryEscape(pair.value))
	}
	return strings.Join(parts, "&")
}

// flattenAll 展平全部参数：顶层键排序后逐个交给 flattenParams。
// 顶层排序只为输出确定性——服务端验签对顶层 ksort（签名算法第 3 步），顶层键序不影响验签。
func flattenAll(params map[string]interface{}) formPairs {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	flat := make(formPairs, 0, len(params))
	for _, key := range keys {
		flattenParams(key, params[key], &flat)
	}
	return flat
}

// flattenParams 把（可能嵌套的）参数展平为 PHP 表单括号记法：payment_method[bank]=ICBC。
// 顶层标量直接成对；*OrderedPairs 按 Keys() 序递归 [k] 后缀。
// 键序保持插入序、绝不排序：后端 parse_str 按到达序重嵌套、json_encode 的键序即验签键序，
// 与 Sign 在原始嵌套参数上的 JSON 化键序逐字对齐是嵌套参数验签通过的关键
// （这也是 out 不用 url.Values 的原因：其 Encode() 会按键排序丢失插入序）。
// 仅 nil 不上行：顶层空串上行后服务端重算同样跳过（等价），
// 嵌套空串必须上行以保住 json_encode 键值形态——Sign 的跳空值仅作用于顶层标量。
func flattenParams(prefix string, v interface{}, out *formPairs) {
	switch value := v.(type) {
	case *OrderedPairs:
		if value == nil {
			return
		}
		for _, key := range value.Keys() {
			flattenParams(prefix+"["+key+"]", value.vals[key], out)
		}
	case string:
		*out = append(*out, formPair{key: prefix, value: value})
	case nil:
		return
	default:
		// 与 Sign 的类型契约一致 fail fast（正常流程 Sign 先于此触发，此为直接调用防御）
		panic(fmt.Sprintf("huanyusdk: 参数 %q 类型不受支持（%T），请传 string、*OrderedPairs 或 nil", prefix, v))
	}
}

// flexInt 兼容字符串/数字两种 JSON 形态的整型。真实后端信封 time 恒为字符串形式的
// 秒级时间戳（TP5 result() 内 input() 默认强转 string，见 huanyu-backend
// application/common/controller/Api.php），但网关/历史数据可能出现裸数字，两种都要能解，
// 否则真实响应会被误判为"平台响应格式异常"。ApiError.Time 对外保持 int64 不变，
// flexInt 只承担 wire 解析层的形态兼容。
type flexInt int64

// UnmarshalJSON 接受带引号 "1756684800" 与裸数字 1756684800；null/缺字段/空串视为 0，
// 其余无法解析的形态返回错误（走"平台响应格式异常"路径，不静默吞掉）。
func (f *flexInt) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	text = strings.Trim(text, `"`)
	if text == "" || text == "null" {
		*f = 0
		return nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("信封 time 解析失败: %s: %w", snippet(data), err)
	}
	*f = flexInt(value)
	return nil
}

// parseEnvelope 解析响应信封 {code, msg, data, time}：非 2xx 或 JSON 解析失败/缺 code
// 返回错误（含截断 body 200 字符）；Code != 1 返回 *ApiError；成功返回 Data
// （data 缺失/null 时为空 map）。响应解析无保序需求，标准库 encoding/json 可用。
func parseEnvelope(statusCode int, body []byte) (map[string]interface{}, error) {
	if statusCode < 200 || statusCode > 299 {
		return nil, fmt.Errorf("平台响应异常（HTTP %d）: %s", statusCode, snippet(body))
	}
	var envelope struct {
		Code *int                   `json:"code"` // 指针区分"缺 code"与 code=0（后者是真实业务失败）
		Msg  string                 `json:"msg"`
		Time flexInt                `json:"time"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code == nil {
		return nil, fmt.Errorf("平台响应格式异常: %s", snippet(body))
	}
	if *envelope.Code != 1 {
		return nil, NewApiError(*envelope.Code, envelope.Msg, int64(envelope.Time))
	}
	if envelope.Data == nil {
		return map[string]interface{}{}, nil
	}
	return envelope.Data, nil
}

// snippet 响应片段截断（对齐 PHP substr($body, 0, 200) 的字节级语义），便于排障且不刷屏。
func snippet(body []byte) string {
	if len(body) <= 200 {
		return string(body)
	}
	return string(body[:200])
}

// randomNonce 用 crypto/rand 生成 16 位字母数字 nonce。
func randomNonce() string {
	buf := make([]byte, nonceLength)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("huanyusdk: 生成 nonce 失败: %v", err))
	}
	out := make([]byte, nonceLength)
	for i, b := range buf {
		out[i] = nonceChars[int(b)%len(nonceChars)]
	}
	return string(out)
}
