package usage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"tokenusage/internal/resp"
)

const maxLen = 1024

// recordBodyChunk 记录 body 原始字符串（最多两个，滚动更新）
func (ctx *RequestCtx) recordBodyChunk(body []byte) {

	if len(body) > maxLen {
		body = body[:maxLen]
	}
	bodybuf := bytes.Clone(body)
	ctx.recentChunks[ctx.chunkIndex%2] = bodybuf
	ctx.chunkIndex++
	if ctx.chunkIndex > 1 {
		ctx.IsStreaming = true
	}
}

// printRecordedBody 在 EndOfStream 时打印记录的 body 内容
func (ctx *RequestCtx) printRecordedBody() {

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
		fmt.Printf("--- Chunk %d ---\n[%s]\n---------", i+1, string(ctx.recentChunks[idx]))
		ctx.parseUsageFromSSE(ctx.recentChunks[idx])
	}

	fmt.Printf("============================\n\n")
}

// parseUsageFromSSE 从 SSE 数据中解析带 usage 字段的报文
func (ctx *RequestCtx) parseUsageFromSSE(body []byte) {

	lines := bytes.Split(body, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}

		jsonPart := bytes.TrimPrefix(line, []byte("data: "))

		if bytes.Equal(bytes.TrimSpace(jsonPart), []byte("[DONE]")) {
			continue
		}

		chunk := &resp.OpenAIResp{}
		if err := json.Unmarshal(jsonPart, chunk); err != nil {
			continue
		}

		if chunk.GetInputToken() > 0 {
			fmt.Printf("[Usage] %s\n", chunk)
		}
	}

}
