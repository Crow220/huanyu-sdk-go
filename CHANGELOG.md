# Changelog

## 1.0.0 - 2026-09-01

首个公开发布。

### 功能

- `Client`：封装平台全部对外端点——`CreateOrder`（唯一下单方法，三要素是否必填由商户配置决定）、`OrderList`、`OrderDetail`（map 风格，id / order_no / merchant_order_no 三选一）、`UploadPaymentProof`、`ConfirmPayment`；自动注入通用参数（api_key / timestamp / nonce / signature），统一解析 `{code, msg, data, time}` 信封（time 经 flexInt64 兼容字符串形态），`code != 1` 返回 `*ApiError`。
- `Signature`：与后端 `MerchantAuth` 真源一致的 MD5 签名，由共享规格仓的后端实测向量锁定（9 组用例）。
- `OrderedPairs`：保插入序键值对——Go map 无序，数组类参数（卖单 `payment_method`）必须用它构造，`Set` 顺序即签名顺序；手写序列化器（避开 `encoding/json` 的 map 排序与 HTML 转义），`\/` 与 U+2028/U+2029 转义与 PHP `json_encode` 逐字节对齐。
- 嵌套参数按 PHP 括号记法上行（`payment_method[bank]=…`，保序 formPairs 发射——`url.Values.Encode()` 会按键排序破坏插入序，不可用），签名在原始嵌套参数上计算。
- `CallbackVerifier`：回调验签（`subtle.ConstantTimeCompare` 恒时比较，重复键拒绝）。
- 参数类型白名单过滤：`CreateOrderParams` / `OrderListFilters` / `OrderDetailQuery`。

### 注意

- 金额参数为 `cny_amount`（字符串，如 `"50.00"`）。
- `MerchantOrderNo` 商户内唯一：重复建单返回"商户单号已存在"，超时可凭同一单号安全重试（示例见 README）。
- `PaymentMethod` 叶子值用空字符串表示未填，不支持 nil（协议限制，见共享 spec）。
- nonce 每次调用自动全新生成（crypto/rand），时间窗内一次性有效。

### 环境要求

- Go 1.22+（零第三方依赖）；CI 矩阵 Go 1.22/1.23/1.24（含 -race）。
