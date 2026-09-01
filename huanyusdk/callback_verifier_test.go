package huanyusdk

import (
	"encoding/json"
	"net/url"
	"os"
	"testing"
)

// 回调验签测试：期望值真源 common/vectors/callback_vectors.json（后端实测生成，共 2 例，
// 为 9 例向量集的第 8、9 例）。覆盖：合法回调通过、篡改字段拒绝、缺/空 signature 拒绝。

// loadCallbackVectors 复用 signature_test.go 的 vectorFile 结构读取回调向量。
func loadCallbackVectors(t *testing.T) vectorFile {
	t.Helper()
	data, err := os.ReadFile("../common/vectors/callback_vectors.json")
	if err != nil {
		t.Fatalf("读取向量文件失败: %v", err)
	}
	var vf vectorFile
	if err := json.Unmarshal(data, &vf); err != nil {
		t.Fatalf("解析向量文件失败: %v", err)
	}
	return vf
}

// vectorToForm 把向量参数 + 期望签名组装为回调表单（平台 POST 的键值对原样形态）。
func vectorToForm(t *testing.T, c vectorCase) url.Values {
	t.Helper()
	params, err := decodeParams(c.Params)
	if err != nil {
		t.Fatalf("解析用例参数失败: %v", err)
	}
	form := url.Values{}
	for key, value := range params {
		s, ok := value.(string)
		if !ok {
			t.Fatalf("回调字段 %q 应为标量字符串，实际 %T", key, value)
		}
		form.Set(key, s)
	}
	form.Set("signature", c.ExpectedSignature)
	return form
}

func TestCallbackVerifierAcceptsValidVectors(t *testing.T) {
	vf := loadCallbackVectors(t)
	for _, c := range vf.Cases {
		t.Run(c.ID, func(t *testing.T) {
			if !NewCallbackVerifier(vf.APISecret).Verify(vectorToForm(t, c)) {
				t.Errorf("回调向量 %s 验签未通过（实现与后端参考行为出现分歧）", c.ID)
			}
		})
	}
}

func TestCallbackVerifierRejectsTamperedField(t *testing.T) {
	form := firstCallbackForm(t)
	form.Set("cny_amount", "99999.00")
	if NewCallbackVerifier(loadCallbackVectors(t).APISecret).Verify(form) {
		t.Error("篡改金额后仍验签通过")
	}
}

func TestCallbackVerifierRejectsMissingSignature(t *testing.T) {
	form := firstCallbackForm(t)
	form.Del("signature")
	if NewCallbackVerifier(loadCallbackVectors(t).APISecret).Verify(form) {
		t.Error("缺 signature 时不应验签通过")
	}
}

func TestCallbackVerifierRejectsEmptySignature(t *testing.T) {
	form := firstCallbackForm(t)
	form.Set("signature", "")
	if NewCallbackVerifier(loadCallbackVectors(t).APISecret).Verify(form) {
		t.Error("signature 为空串时不应验签通过")
	}
}

// firstCallbackForm 取 callback-full 向量的表单拷贝，用例间不共享可变状态。
func firstCallbackForm(t *testing.T) url.Values {
	t.Helper()
	vf := loadCallbackVectors(t)
	for _, c := range vf.Cases {
		if c.ID == "callback-full" {
			return vectorToForm(t, c)
		}
	}
	t.Fatalf("callback_vectors.json 缺少 callback-full 向量")
	return nil
}
