package server

import (
	"encoding/json"
	"net/http"
	"tokenusage/pkg/logger"
)

// LogLevelHandler 日志等级 HTTP 处理器
type LogLevelHandler struct{}

// NewLogLevelHandler 创建日志等级处理器
func NewLogLevelHandler() *LogLevelHandler {
	return &LogLevelHandler{}
}

// ServeHTTP 处理日志等级请求
// GET /log/level - 获取当前日志等级
// PUT /log/level - 设置日志等级（body: {"level": "debug|info|warn|error"}）
func (h *LogLevelHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodPut:
		h.handlePut(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *LogLevelHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	resp := map[string]string{
		"level": logger.GetLevel().String(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *LogLevelHandler) handlePut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level string `json:"level"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	logger.SetLevel(req.Level)

	resp := map[string]string{
		"level": logger.GetLevel().String(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}