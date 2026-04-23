package util

import corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

func GetHeaders(headers *corev3.HeaderMap, data map[string]string) {
	for _, h := range headers.GetHeaders() {
		if len(h.Value) > 0 && IsContains(data, h.GetKey()) {
			data[h.GetKey()] = h.GetValue()
		}
	}
}

func IsContains(dict map[string]string, key string) bool {
	_, ok := dict[key]
	return ok
}
