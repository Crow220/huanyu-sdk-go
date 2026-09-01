# huanyu-sdk-go

PISCES商户平台官方 Go SDK。

## 安装

```
go get github.com/crow220/huanyu-sdk-go
```

## 快速上手

```go
import "github.com/crow220/huanyu-sdk-go/huanyusdk"

// 默认指向生产 baseURL（30s 超时）；联调换环境用 WithBaseURL，自定义 http.Client 用 WithHTTPClient
client := huanyusdk.NewClient("你的api_key", "你的api_secret")

// 创建订单（三要素字段是否必填由商户配置决定）
params := &huanyusdk.CreateOrderParams{
	OrderType:       "1",            // 1=买入 2=卖出
	CnyAmount:       "100.00",
	MerchantOrderNo: "M20260831001", // 商户内唯一，重复会被拒绝
}
order, err := client.CreateOrder(params)
if err != nil {
	log.Fatal(err) // 业务失败为 *huanyusdk.ApiError，处理方式见下文「重要注意事项」
}
// order["result_status"] == "pending_identity" 时，引导用户访问 order["identity_url"] 补全身份信息

// 卖出示例：PaymentMethod 必填，请用 NewOrderedPairs().Set(...) 链式构造——
// 字段 Set 顺序即签名顺序（服务端按字段出现序验签），不要用 Go map（遍历无序，签名不稳定）
// 注意换成卖出语义 + 新商户单号：同商户重复单号会被"商户单号已存在"拒绝
params.OrderType = "2"
params.MerchantOrderNo = "M20260831002"
params.PaymentMethod = huanyusdk.NewOrderedPairs().
	Set("bank", "中国工商银行").
	Set("sub_bank", "杭州某某支行").
	Set("card_number", "6222020200112233445").
	Set("real_name", "张三")
sellOrder, err := client.CreateOrder(params)

// 查询（后续示例省略 err 检查）
list, err := client.OrderList(&huanyusdk.OrderListFilters{
	Status: "paid,confirmed",
	Page:   "1",
	Limit:  "20",
})

detail, err := client.OrderDetail(map[string]string{ // id / order_no / merchant_order_no 三选一
	"order_no": order["order_no"].(string),
})

// 卖单确认付款（paymentProof 传空串表示不上行该字段）/ 上传凭证
_, err = client.ConfirmPayment(order["order_no"].(string), "")
_, err = client.UploadPaymentProof(order["order_no"].(string), "https://your.cdn/proof.png")
```

## 回调处理

```go
import (
	"net/http"

	"github.com/crow220/huanyu-sdk-go/huanyusdk"
)

verifier := huanyusdk.NewCallbackVerifier("你的api_secret")

func handleCallback(w http.ResponseWriter, r *http.Request) {
	// 解析 application/x-www-form-urlencoded 表单后验签
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !verifier.Verify(r.PostForm) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// ...业务处理（回调仅在订单 completed 时推送）
	w.Write([]byte("success")) // 必须响应 HTTP 200 且含 success，否则平台按 5/30/120/600s 共推送 5 次（首次 + 4 次重试）
}
```

## 重要注意事项

- **merchant_order_no 商户内唯一**：同一商户重复单号建单返回"商户单号已存在"错误（不同商户间可重复）。网络超时后可用同一单号安全重试——若返回"已存在"，说明首单已建成，请按单号查单确认状态：

```go
import (
	"errors"
	"strings"
)

order, err := client.CreateOrder(params)
if err != nil {
	var apiErr *huanyusdk.ApiError
	if errors.As(err, &apiErr) && strings.Contains(apiErr.Msg, "商户单号已存在") {
		// 首单已建成：按商户单号查单确认状态即可，不要重复下单
		order, err = client.OrderDetail(map[string]string{
			"merchant_order_no": "M20260831001",
		})
		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Fatal(err)
	}
}
```

- **数组参数叶子值用空字符串表示"未填"**：卖出单 PaymentMethod 的可选字段请 `Set("sub_bank", "")` 显式传空串，不要留空不 Set；叶子值无法表达 null（表单线路的协议限制，见 common/spec/signature.md"已知限制"）。
- **nonce 自动生成**：平台要求每个请求的 nonce 在 10 分钟窗口内一次性有效（防重放）。SDK 每次调用都会自动生成全新的 timestamp/nonce/signature，失败后直接再次调用即可，无需（也不要）缓存复用请求参数。
- timestamp 为秒级时间戳，本机时钟偏差超过 ±300 秒会验签失败。

## 要求

- Go 1.22+
