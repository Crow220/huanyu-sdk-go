package huanyusdk

import (
	"reflect"
	"testing"
)

// 参数类型测试：ToSignParams 零值跳过 / PaymentMethod 非 nil 才带 / 白名单过滤。

func TestCreateOrderParamsToSignParamsSkipsZeroValues(t *testing.T) {
	params := &CreateOrderParams{
		OrderType:    "1",
		CnyAmount:    "100.00",
		CustomerName: "张三",
		Remark:       "", // 空串跳过
		// IdCard / Mobile / MerchantOrderNo 零值跳过；PaymentMethod nil 不带
	}
	got := params.ToSignParams()
	want := map[string]interface{}{
		"order_type":    "1",
		"cny_amount":    "100.00",
		"customer_name": "张三",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("零值字段应被跳过：期望 %v，实际 %v", want, got)
	}
}

func TestCreateOrderParamsToSignParamsCarriesPaymentMethod(t *testing.T) {
	paymentMethod := NewOrderedPairs().
		Set("real_name", "李四").
		Set("card_number", "6217000000001234567").
		Set("bank", "中国银行").
		Set("sub_bank", "深圳分行")
	got := (&CreateOrderParams{OrderType: "2", CnyAmount: "500.50", PaymentMethod: paymentMethod}).ToSignParams()
	if got["payment_method"] != paymentMethod {
		t.Error("payment_method 应原样携带 *OrderedPairs（保序容器直接参与签名）")
	}
}

func TestCreateOrderParamsNilReceiverReturnsEmptyParams(t *testing.T) {
	var params *CreateOrderParams
	if got := params.ToSignParams(); len(got) != 0 {
		t.Errorf("nil 接收者应返回空 map，实际 %v", got)
	}
}

func TestOrderListFiltersToSignParamsSkipsZeroValues(t *testing.T) {
	got := (&OrderListFilters{
		Page:         "2",
		Status:       "pending,paid",
		MaxCnyAmount: "500.00",
	}).ToSignParams()
	want := map[string]interface{}{
		"page":           "2",
		"status":         "pending,paid",
		"max_cny_amount": "500.00",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("零值字段应被跳过：期望 %v，实际 %v", want, got)
	}
}

func TestOrderListFiltersAllFieldsMappedToSpecNames(t *testing.T) {
	// 10 个白名单字段逐一映射到 spec/api.md 的参数名（防止字段增删后映射漂移）
	got := (&OrderListFilters{
		Page:            "1",
		Limit:           "20",
		Status:          "paid",
		OrderType:       "1",
		StartTime:       "2026-08-01",
		EndTime:         "2026-08-31",
		OrderNo:         "HY001",
		MerchantOrderNo: "M001",
		MinCnyAmount:    "100.00",
		MaxCnyAmount:    "500.00",
	}).ToSignParams()
	want := map[string]interface{}{
		"page":              "1",
		"limit":             "20",
		"status":            "paid",
		"order_type":        "1",
		"start_time":        "2026-08-01",
		"end_time":          "2026-08-31",
		"order_no":          "HY001",
		"merchant_order_no": "M001",
		"min_cny_amount":    "100.00",
		"max_cny_amount":    "500.00",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("字段映射不完整：期望 %v，实际 %v", want, got)
	}
}

func TestOrderDetailQueryFiltersWhitelistAndSkipsEmpty(t *testing.T) {
	got := OrderDetailQuery(map[string]string{
		"id":                "123",
		"order_no":          "HY001",
		"merchant_order_no": "", // 空串跳过
		"hack":              "evil",
		"page":              "9", // 其它端点字段不应混入
	})
	want := map[string]interface{}{"id": "123", "order_no": "HY001"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("白名单过滤不符：期望 %v，实际 %v", want, got)
	}
}
