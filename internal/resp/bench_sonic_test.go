//go:build sonicjson

package resp

import (
	"bytes"
	_ "embed"
	"testing"
)

//go:embed testdata/sse_events.jsonl
var testData []byte

var sseEvents [][]byte

func init() {
	for _, line := range bytes.Split(testData, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) > 6 {
			sseEvents = append(sseEvents, line)
		}
	}
}

// BenchmarkParseUsage_sonic 测试 sonic JIT 反序列化方案
func BenchmarkParseUsage_sonic(b *testing.B) {
	if len(sseEvents) == 0 {
		b.Fatal("no test data; run: python3 scripts/gen_testdata.py")
	}
	b.SetBytes(int64(len(testData)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseUsage(testData, "/v1/chat/completions")
	}
}

// BenchmarkExtract_single_sonic 单条 JSON extract（小 / 大各一组）
func BenchmarkExtract_single_sonic(b *testing.B) {
	small := findEventBySize(sseEvents, 0, 1000)
	large := findEventBySize(sseEvents, 50000, -1)

	b.Run("small", func(b *testing.B) {
		if small == nil {
			b.Skip("no small event found")
		}
		data := stripSSEPrefix(small)
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			extract(data)
		}
	})
	b.Run("large", func(b *testing.B) {
		if large == nil {
			b.Skip("no large event found")
		}
		data := stripSSEPrefix(large)
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			extract(data)
		}
	})
}
