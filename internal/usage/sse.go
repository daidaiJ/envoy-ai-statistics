package usage

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// recordBodyChunk 记录 body 原始字符串（最多两个，滚动更新）
func (ctx *RequestCtx) recordBodyChunk(body []byte) {
	ctx.bodyMu.Lock()
	defer ctx.bodyMu.Unlock()

	maxLen := 1000
	bodyStr := string(body)
	if len(bodyStr) > maxLen {
		bodyStr = bodyStr[:maxLen] + "...(truncated)"
	}

	ctx.recentChunks[ctx.chunkIndex%2] = bodyStr
	ctx.chunkIndex++
}

// printRecordedBody 在 EndOfStream 时打印记录的 body 内容
func (ctx *RequestCtx) printRecordedBody() {
	ctx.bodyMu.Lock()
	defer ctx.bodyMu.Unlock()

	streamType := "非流式"
	if ctx.IsStreaming {
		streamType = "流式"
	}
	fmt.Printf("\n========== 响应结束 ==========\n")
	fmt.Printf("响应格式: %s\n", streamType)
	fmt.Printf("最近的 body chunks:\n")

	count := ctx.chunkIndex
	if count > 2 {
		count = 2
	}

	for i := 0; i < count; i++ {
		idx := (ctx.chunkIndex - count + i) % 2
		if idx < 0 {
			idx += 2
		}
		fmt.Printf("--- Chunk %d ---\n%s\n", i+1, ctx.recentChunks[idx])
	}

	if len(ctx.bodyBuf) > 0 {
		fmt.Printf("\n--- 完整响应体 ---\n")
		fmt.Printf("%s\n", string(ctx.bodyBuf))
	}
	fmt.Printf("============================\n\n")
}

// parseUsageFromSSE 从 SSE 数据中解析带 usage 字段的报文
func (ctx *RequestCtx) parseUsageFromSSE() {
	ctx.bodyMu.Lock()
	defer ctx.bodyMu.Unlock()

	if len(ctx.bodyBuf) == 0 {
		return
	}

	fmt.Printf("\n========== Usage 信息 ==========\n")

	lines := bytes.Split(ctx.bodyBuf, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}

		jsonPart := bytes.TrimPrefix(line, []byte("data: "))

		if bytes.Equal(bytes.TrimSpace(jsonPart), []byte("[DONE]")) {
			continue
		}

		var chunk map[string]any
		if err := json.Unmarshal(jsonPart, &chunk); err != nil {
			continue
		}

		if usage, ok := chunk["usage"].(map[string]any); ok {
			usageJSON, _ := json.MarshalIndent(usage, "", "  ")
			fmt.Printf("发现 usage: %s\n", string(usageJSON))
		}
	}

	fmt.Printf("================================\n")
}