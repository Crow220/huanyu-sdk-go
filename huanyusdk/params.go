package huanyusdk

// CreateOrderParams 创建订单参数（POST /merchant/createOrder）。
// 结构体字段即白名单（spec/api.md），未知字段无法混入；ToSignParams 跳过零值字段。
// 金额等数字请以字符串形式传入（如 "100.50"），规避各语言浮点序列化差异。
type CreateOrderParams struct {
	OrderType       string        // 1=买入 2=卖出
	CnyAmount       string        // CNY 金额，大于 0，如 "100.50"
	PaymentMethod   *OrderedPairs // 卖单必填：bank/sub_bank/card_number/real_name，非 nil 才参与
	CustomerName    string        // 三要素之一，是否必填由商户 identity_required 配置决定
	IdCard          string        // 三要素之一
	Mobile          string        // 三要素之一
	Remark          string        // 选填备注
	MerchantOrderNo string        // 商户内唯一；重复单号建单返回"商户单号已存在"，超时可用同号安全重试
	CallbackUrl     string        // 本单回调地址（http/https，限长255）；未设置用商户默认回调地址
}

// ToSignParams 输出参与签名与上行的参数：跳过零值字段，PaymentMethod 非 nil 才携带。
// nil 接收者返回空 map（等价空参数请求，由服务端校验缺参）。
func (p *CreateOrderParams) ToSignParams() map[string]interface{} {
	if p == nil {
		return map[string]interface{}{}
	}
	params := make(map[string]interface{})
	if p.OrderType != "" {
		params["order_type"] = p.OrderType
	}
	if p.CnyAmount != "" {
		params["cny_amount"] = p.CnyAmount
	}
	if p.PaymentMethod != nil {
		params["payment_method"] = p.PaymentMethod
	}
	if p.CustomerName != "" {
		params["customer_name"] = p.CustomerName
	}
	if p.IdCard != "" {
		params["id_card"] = p.IdCard
	}
	if p.Mobile != "" {
		params["mobile"] = p.Mobile
	}
	if p.Remark != "" {
		params["remark"] = p.Remark
	}
	if p.MerchantOrderNo != "" {
		params["merchant_order_no"] = p.MerchantOrderNo
	}
	if p.CallbackUrl != "" {
		params["callback_url"] = p.CallbackUrl
	}
	return params
}

// OrderListFilters 订单列表过滤参数（GET /merchant/orderListApi），
// 10 个字段即白名单（spec/api.md），ToSignParams 跳过零值字段。
type OrderListFilters struct {
	Page            string // 页码，默认 1
	Limit           string // 每页条数，默认 20
	Status          string // 订单状态，支持逗号分隔多状态
	OrderType       string // 1=买入 2=卖出
	StartTime       string // 开始日期（Y-m-d）
	EndTime         string // 结束日期（Y-m-d）
	OrderNo         string // 系统订单号
	MerchantOrderNo string // 商户单号（模糊）
	MinCnyAmount    string // 最小 CNY 金额
	MaxCnyAmount    string // 最大 CNY 金额
}

// ToSignParams 输出参与签名与上行的参数：跳过零值字段。nil 接收者返回空 map。
func (f *OrderListFilters) ToSignParams() map[string]interface{} {
	if f == nil {
		return map[string]interface{}{}
	}
	params := make(map[string]interface{})
	if f.Page != "" {
		params["page"] = f.Page
	}
	if f.Limit != "" {
		params["limit"] = f.Limit
	}
	if f.Status != "" {
		params["status"] = f.Status
	}
	if f.OrderType != "" {
		params["order_type"] = f.OrderType
	}
	if f.StartTime != "" {
		params["start_time"] = f.StartTime
	}
	if f.EndTime != "" {
		params["end_time"] = f.EndTime
	}
	if f.OrderNo != "" {
		params["order_no"] = f.OrderNo
	}
	if f.MerchantOrderNo != "" {
		params["merchant_order_no"] = f.MerchantOrderNo
	}
	if f.MinCnyAmount != "" {
		params["min_cny_amount"] = f.MinCnyAmount
	}
	if f.MaxCnyAmount != "" {
		params["max_cny_amount"] = f.MaxCnyAmount
	}
	return params
}

// orderDetailFields OrderDetailQuery 白名单（id / order_no / merchant_order_no 三选一）。
var orderDetailFields = map[string]bool{
	"id":                true,
	"order_no":          true,
	"merchant_order_no": true,
}

// OrderDetailQuery 过滤订单详情查询条件（GET /merchant/orderDetailApi）：
// map 入参不像结构体天然白名单，须显式过滤——只保留白名单键且跳过空串。
func OrderDetailQuery(query map[string]string) map[string]interface{} {
	params := make(map[string]interface{})
	for key, value := range query {
		if orderDetailFields[key] && value != "" {
			params[key] = value
		}
	}
	return params
}
