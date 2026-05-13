package resp

// bench_helper_test.go — benchmark 共享辅助函数（无 build tag，所有变体共用）

// findEventBySize 在 SSE 事件列表中按 "data: " 后的 JSON 长度筛选。
// maxLen <= 0 表示不限上限。
func findEventBySize(events [][]byte, minLen, maxLen int) []byte {
	for _, ev := range events {
		payload := stripSSEPrefix(ev)
		n := len(payload)
		if n < minLen {
			continue
		}
		if maxLen > 0 && n > maxLen {
			continue
		}
		return ev
	}
	return nil
}

// stripSSEPrefix 去掉 "data: " 前缀，返回纯 JSON 部分
func stripSSEPrefix(line []byte) []byte {
	if len(line) > 6 && string(line[:6]) == "data: " {
		return line[6:]
	}
	return line
}
