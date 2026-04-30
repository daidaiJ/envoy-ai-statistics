package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Level 动态日志等级
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String 返回等级字符串
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel 从字符串解析等级
func ParseLevel(s string) Level {
	switch s {
	case "debug", "DEBUG":
		return LevelDebug
	case "info", "INFO":
		return LevelInfo
	case "warn", "WARN", "warning", "WARNING":
		return LevelWarn
	case "error", "ERROR":
		return LevelError
	default:
		return LevelInfo
	}
}

// toSlogLevel 转换为 slog.Level
func (l Level) toSlogLevel() slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// toZerologLevel 转换为 zerolog.Level
func (l Level) toZerologLevel() zerolog.Level {
	switch l {
	case LevelDebug:
		return zerolog.DebugLevel
	case LevelInfo:
		return zerolog.InfoLevel
	case LevelWarn:
		return zerolog.WarnLevel
	case LevelError:
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// dynamicHandler 支持动态等级切换的 slog.Handler
type dynamicHandler struct {
	mu       sync.RWMutex
	level    Level
	writer   io.Writer
	attrs    []slog.Attr
	groups   []string
	timezone *time.Location
}

var (
	defaultLogger *slog.Logger
	defaultLevel  = LevelInfo
	handler       *dynamicHandler
)

// Init 初始化日志
func Init(level string) {
	l := ParseLevel(level)

	// 从环境变量加载时区
	timezone := loadTimezone()

	// 创建 zerolog ConsoleWriter（带颜色和时间格式）
	zlWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "2006-01-02 15:04:05.000",
	}

	handler = &dynamicHandler{
		level:    l,
		writer:   zlWriter,
		timezone: timezone,
	}

	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// loadTimezone 从环境变量加载时区
func loadTimezone() *time.Location {
	tz := os.Getenv("TZ")
	if tz == "" {
		tz = os.Getenv("TIMEZONE")
	}
	if tz == "" {
		return time.Local
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Local
	}
	return loc
}

// SetLevel 运行时动态设置日志等级
func SetLevel(level string) {
	l := ParseLevel(level)

	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.level = l

	slog.Info("日志等级已更新", "level", l.String())
}

// GetLevel 获取当前日志等级
func GetLevel() Level {
	handler.mu.RLock()
	defer handler.mu.RUnlock()
	return handler.level
}

// slog.Handler 接口实现

func (h *dynamicHandler) Enabled(_ context.Context, level slog.Level) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var currentLevel slog.Level
	switch h.level {
	case LevelDebug:
		currentLevel = slog.LevelDebug
	case LevelInfo:
		currentLevel = slog.LevelInfo
	case LevelWarn:
		currentLevel = slog.LevelWarn
	case LevelError:
		currentLevel = slog.LevelError
	default:
		currentLevel = slog.LevelInfo
	}

	return level >= currentLevel
}

func (h *dynamicHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 获取 caller 信息
	_, file, line, ok := runtime.Caller(4)
	if !ok {
		_, file, line, _ = runtime.Caller(3)
	}

	// 格式化时间（使用配置的时区）
	t := r.Time.In(h.timezone)

	// 构建日志输出
	levelStr := r.Level.String()
	timeStr := t.Format("2006-01-02 15:04:05.000")

	var msg string
	if len(h.groups) > 0 {
		msg = fmt.Sprintf("[%s] %s %s:%d: %s", levelStr, timeStr, file, line, r.Message)
	} else {
		msg = fmt.Sprintf("[%s] %s %s:%d: %s", levelStr, timeStr, file, line, r.Message)
	}

	// 添加属性
	var attrs []string
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, fmt.Sprintf("%s=%v", a.Key, a.Value.Any()))
		return true
	})

	for _, a := range h.attrs {
		attrs = append(attrs, fmt.Sprintf("%s=%v", a.Key, a.Value.Any()))
	}

	if len(attrs) > 0 {
		msg += " " + joinAttrs(attrs)
	}

	fmt.Fprintln(h.writer, msg)
	return nil
}

func (h *dynamicHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.mu.RLock()
	defer h.mu.RUnlock()

	newHandler := &dynamicHandler{
		level:    h.level,
		writer:   h.writer,
		attrs:    append(h.attrs, attrs...),
		groups:   h.groups,
		timezone: h.timezone,
	}
	return newHandler
}

func (h *dynamicHandler) WithGroup(name string) slog.Handler {
	h.mu.RLock()
	defer h.mu.RUnlock()

	newHandler := &dynamicHandler{
		level:    h.level,
		writer:   h.writer,
		attrs:    h.attrs,
		groups:   append(h.groups, name),
		timezone: h.timezone,
	}
	return newHandler
}

func joinAttrs(attrs []string) string {
	result := ""
	for i, a := range attrs {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}

// 便捷方法

func Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

func Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

func Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

func Error(msg string, args ...any) {
	slog.Error(msg, args...)
}

func With(args ...any) *slog.Logger {
	return slog.With(args...)
}