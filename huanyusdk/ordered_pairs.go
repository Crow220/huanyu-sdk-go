package huanyusdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// OrderedPairs 保插入序的键值对集（Go map 无序；数组类参数如卖单 payment_method 必须用它构造，
// 字段出现序即 PHP 端 json_encode 的键序，直接影响签名）。
type OrderedPairs struct {
	keys []string
	vals map[string]string
}

// NewOrderedPairs 创建空的保序键值对集。
func NewOrderedPairs() *OrderedPairs {
	return &OrderedPairs{vals: make(map[string]string)}
}

// Set 链式写入键值；重复 key 覆盖旧值且不会在序列化中出现两次。
// 零值 OrderedPairs 也可直接 Set（内部按需初始化）。
func (p *OrderedPairs) Set(key, value string) *OrderedPairs {
	if p.vals == nil {
		p.vals = make(map[string]string)
	}
	if _, exists := p.vals[key]; !exists {
		p.keys = append(p.keys, key)
	}
	p.vals[key] = value
	return p
}

// Keys 返回键出现顺序的快照（拷贝，调用方可安全修改）。
func (p *OrderedPairs) Keys() []string {
	snapshot := make([]string, len(p.keys))
	copy(snapshot, p.keys)
	return snapshot
}

// MarshalJSON 手写序列化，逐步对应 spec/signature.md 第 2 步。
// 不用 encoding/json 编码：它对 map 按键排序、且默认把 < > & 转义为 \u003c 形式，
// 均会破坏与 PHP json_encode($v, JSON_UNESCAPED_UNICODE) 的字节级一致。
// 输出 {"k":"v","k2":"v2"}：无空格、保 keys 序、UTF-8 原样；
// 转义集与 PHP json_encode 实测对齐：'/' → \/、'"' → \"、'\' → \\、
// \b \f \n \r \t 短转义、其余 <0x20 控制字符 → \u00xx（小写十六进制）、
// U+2028/U+2029 → \u2028/\u2029（PHP 固有转义）、< > & 原样。
// 值均为 string（本 SDK 的数组参数叶子值按 PHP 语义为字符串）。
// 输入须为合法 UTF-8，否则行为未定义（截断的多字节序列会被原样输出）。
//
// 注意：调用方须直接使用本方法返回值；不要把 *OrderedPairs 交给 json.Marshal——
// encoding/json 会对 MarshalJSON 的输出再压缩并做 HTML 转义，< > & 会变成 \u003c 形式。
func (p *OrderedPairs) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range p.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		writeJSONString(&buf, key)
		buf.WriteByte(':')
		writeJSONString(&buf, p.vals[key])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// writeJSONString 按上述转义集写入带引号的 JSON 字符串。
// 逐字节扫描是安全的：UTF-8 多字节序列的每个字节都 >= 0x80，原样落入 default 分支；
// 唯一例外是 U+2028/U+2029（见下方特判）。
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '/':
			buf.WriteString(`\/`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			switch {
			case c < 0x20:
				// PHP json_encode 用小写十六进制输出控制字符
				fmt.Fprintf(buf, `\u%04x`, c)
			case c == 0xE2 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xA8 || s[i+2] == 0xA9):
				// U+2028（行分隔符）/U+2029（段分隔符），UTF-8 三字节 E2 80 A8/A9：
				// PHP json_encode 即使 JSON_UNESCAPED_UNICODE 也固有转义为 \u2028/\u2029，
				// 粘贴的收款人名等合法输入可含此类字符，须逐字节对齐真源。
				// 须确认后两字节才转义，其余 E2 开头的合法序列（—、… 等）原样放行。
				if s[i+2] == 0xA8 {
					buf.WriteString(`\u2028`)
				} else {
					buf.WriteString(`\u2029`)
				}
				i += 2 // 跳过该三字节序列的剩余两字节
			default:
				buf.WriteByte(c) // UTF-8 原样（中文、< > & 均不转义）
			}
		}
	}
	buf.WriteByte('"')
}

// UnmarshalJSON 用 json.Decoder + Token 流式读取 {"k":"v",...}：
// Token 不会返回 ':' 与 ','（结构分隔符由 Decoder 内部消化），
// 天然按出现顺序返回 键→值→键→值，逐个 Set 即保序填充（decodeParams 依赖此特性）。
// 叶子值按 PHP 语义为字符串；为容错同时接受 JSON 数字/布尔（按字面转字符串，保住尾零形态）。
// 解码前会清空已有内容，重复键后者覆盖。
func (p *OrderedPairs) UnmarshalJSON(data []byte) error {
	p.keys = nil
	p.vals = make(map[string]string)

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("huanyusdk: 解析 JSON 失败: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("huanyusdk: OrderedPairs 只接受 JSON 对象，得到 %v", tok)
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("huanyusdk: 解析 JSON 失败: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("huanyusdk: 对象键必须是字符串，得到 %v", keyTok)
		}
		valTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("huanyusdk: 解析 JSON 失败: %w", err)
		}
		switch v := valTok.(type) {
		case string:
			p.Set(key, v)
		case json.Number:
			p.Set(key, v.String())
		case bool:
			p.Set(key, strconv.FormatBool(v))
		default:
			return fmt.Errorf("huanyusdk: 字段 %q 的值必须是标量，得到 %v", key, valTok)
		}
	}
	if _, err := dec.Token(); err != nil { // 消费 '}'
		return fmt.Errorf("huanyusdk: 解析 JSON 失败: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("huanyusdk: JSON 对象后存在多余数据")
	}
	return nil
}
