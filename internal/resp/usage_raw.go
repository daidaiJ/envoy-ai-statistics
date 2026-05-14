package resp

import (
	"bytes"
	"time"

	"tokenusage/pkg/logger"
)

// UsageRaw 从响应体中提取的 token 用量数据（轻量级，避免完整结构体分配）
type UsageRaw struct {
	Model       string
	InputToken  int64
	OutputToken int64
	CachedToken int64
	Duration    time.Duration // 请求头到达 → 响应结束（EndOfStream）
	TTFT        time.Duration // 请求头到达 → 首个响应 body chunk（首 token 延迟）
}

var emptyUsageResult = &UsageRaw{}

// ParseUsage 从 SSE 响应体中解析 token usage。
//
// SSE 协议格式：每个事件以 "data: " 前缀，事件间以 "\n" 分隔。
// 流结束标记为 "data: [DONE]"。
//
// 返回 nil 表示未找到有效的 usage 数据。
func ParseUsage(body []byte, path string) *UsageRaw {
	lines := splitLines(body)
	for _, line := range lines {
		line = trimSpace(line)
		if len(line) == 0 {
			continue
		}

		jsonPart := trimDataPrefix(line)
		if len(trimSpace(jsonPart)) == 9 && string(trimSpace(jsonPart)) == "[DONE]" {
			continue
		}

		result := extract(jsonPart)
		if result == nil {
			logger.Warn("解析响应JSON失败", "path", path, "len", len(jsonPart))
			continue
		}

		if result.InputToken > 0 {
			return result
		}
	}
	return nil
}

// ExtractModel 从 JSON 字节中快速提取 model 字段值。
// 使用字节级扫描，不依赖 JSON 解析器，适合高频调用。
// 返回空字符串表示未找到或格式异常。
func ExtractModel(data []byte) string {
	idx := bytes.Index(data, []byte(`"model"`))
	if idx < 0 {
		return ""
	}
	rest := data[idx+7:]
	// 跳过空白和冒号
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == ':') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:] // 跳过开头引号
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return string(rest[:end])
}

// splitLines 按换行符分割，避免 bytes.Split 的额外分配
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// trimSpace 去除首尾空白字符
func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// trimDataPrefix 去除 SSE "data: " 前缀
func trimDataPrefix(line []byte) []byte {
	if len(line) > 6 && string(line[:6]) == "data: " {
		return line[6:]
	}
	return line
}
