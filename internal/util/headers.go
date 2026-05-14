package util

import (
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

// GetHeaders 从 Envoy HeaderMap 中提取指定 key 的值（大小写不敏感）
func GetHeaders(headers *corev3.HeaderMap, data map[string]string) {
	for _, h := range headers.GetHeaders() {
		if len(h.Value) == 0 {
			continue
		}
		key := strings.ToLower(h.GetKey())
		if _, ok := data[key]; ok {
			data[key] = h.GetValue()
		}
	}
}
