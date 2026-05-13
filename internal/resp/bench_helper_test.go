package resp

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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

// preExtractJSONs 从 SSE 事件中预提取所有纯 JSON 字节切片，
// 用于 batch benchmark，避免每次迭代重复做行分割和前缀剥离。
func preExtractJSONs(events [][]byte) [][]byte {
	out := make([][]byte, 0, len(events))
	for _, ev := range events {
		out = append(out, stripSSEPrefix(ev))
	}
	return out
}

// totalJSONBytes 计算 JSON 字节切片的总字节数。
func totalJSONBytes(jsons [][]byte) int64 {
	var n int64
	for _, j := range jsons {
		n += int64(len(j))
	}
	return n
}

// goldenEntry 对应 correctness_golden.json 中 results 数组的每个元素。
type goldenEntry struct {
	Model   string `json:"model"`
	Input   int64  `json:"input"`
	Output  int64  `json:"output"`
	Cached  int64  `json:"cached"`
}

// goldenFile 对应 correctness_golden.json 的完整结构。
type goldenFile struct {
	File    string        `json:"file"`
	Count   int           `json:"count"`
	Results []goldenEntry `json:"results"`
	Summary struct {
		TotalInput  int64 `json:"total_input"`
		TotalOutput int64 `json:"total_output"`
		TotalCached int64 `json:"total_cached"`
	} `json:"summary"`
}

// loadGolden 从 testdata/correctness_golden.json 加载期望结果。
func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	// 定位 testdata 目录（相对于当前源文件）
	_, src, _, _ := runtime.Caller(0)
	goldenPath := filepath.Join(filepath.Dir(src), "testdata", "correctness_golden.json")
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("load golden file: %v (run: python3 scripts/gen_testdata.py)", err)
	}
	var g goldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("parse golden file: %v", err)
	}
	return g
}

// runCorrectnessTest 通用正确性验证：用 extractFn 解析全部事件，与 golden 文件逐条对比。
func runCorrectnessTest(t *testing.T, label string, events [][]byte, extractFn func([]byte) *UsageRaw) {
	t.Helper()
	golden := loadGolden(t)

	if len(events) != len(golden.Results) {
		t.Fatalf("event count mismatch: got %d, golden %d", len(events), len(golden.Results))
	}

	var totalInput, totalOutput, totalCached int64
	var mismatches int

	for i, ev := range events {
		data := stripSSEPrefix(ev)
		r := extractFn(data)
		g := golden.Results[i]

		if r == nil {
			t.Errorf("event %d: extract returned nil", i)
			mismatches++
			continue
		}

		if r.Model != g.Model || r.InputToken != g.Input || r.OutputToken != g.Output || r.CachedToken != g.Cached {
			if mismatches < 10 { // 只打印前 10 条
				t.Errorf("event %d: mismatch\n  got:      model=%q input=%d output=%d cached=%d\n  expected: model=%q input=%d output=%d cached=%d",
					i, r.Model, r.InputToken, r.OutputToken, r.CachedToken,
					g.Model, g.Input, g.Output, g.Cached)
			}
			mismatches++
		}

		totalInput += r.InputToken
		totalOutput += r.OutputToken
		totalCached += r.CachedToken
	}

	// 累加汇总对比
	if totalInput != golden.Summary.TotalInput {
		t.Errorf("total input mismatch: got %d, expected %d (delta %d)", totalInput, golden.Summary.TotalInput, totalInput-golden.Summary.TotalInput)
	}
	if totalOutput != golden.Summary.TotalOutput {
		t.Errorf("total output mismatch: got %d, expected %d (delta %d)", totalOutput, golden.Summary.TotalOutput, totalOutput-golden.Summary.TotalOutput)
	}
	if totalCached != golden.Summary.TotalCached {
		t.Errorf("total cached mismatch: got %d, expected %d (delta %d)", totalCached, golden.Summary.TotalCached, totalCached-golden.Summary.TotalCached)
	}

	if mismatches == 0 {
		fmt.Fprintf(os.Stderr, ">> [%s] ✅ %d events all match, totals: input=%d output=%d cached=%d\n",
			label, len(events), totalInput, totalOutput, totalCached)
	} else {
		fmt.Fprintf(os.Stderr, ">> [%s] ❌ %d/%d events mismatch\n", label, mismatches, len(events))
	}
}
