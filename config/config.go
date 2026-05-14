package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 聚合器配置
type Config struct {
	Redis            RedisConfig      `yaml:"redis"`
	Aggr             AggrConfig       `yaml:"aggregator"`
	Log              LogConfig        `yaml:"log"`
	GRPCMaxRecvSize  int              `yaml:"grpc_max_recv_size"`  // gRPC 最大接收消息大小（字节），0 表示使用默认值 64MB
	MaskLen          int              `yaml:"mask_len"`           // SK 日志掩码保留尾部字符数，0 表示全部掩码 "***"
	StreamOptions    StreamOptionsCfg `yaml:"stream_options"`     // 流式请求 stream_options 注入配置
	Exporter         ExporterCfg      `yaml:"exporter"`           // 数据导出配置
}

// LogConfig 日志配置
type LogConfig struct {
	Level string `yaml:"level"` // debug, info, warn, error
}

// RedisConfig Redis 连接配置
type RedisConfig struct {
	Addr     string `yaml:"addr"`     // Redis 地址，如 "localhost:6379"
	Password string `yaml:"password"` // Redis 密码
	DB       int    `yaml:"db"`       // Redis 数据库编号
}

// AggrConfig 聚合器配置
type AggrConfig struct {
	WindowDuration time.Duration `yaml:"window_duration"` // 聚合窗口时长，如 "30s"
	Enabled        bool          `yaml:"enabled"`         // 是否启用时间窗口聚合，false 则每条记录直接导出
	ChannelSize    int           `yaml:"channel_size"`    // record channel 缓冲大小
}

// StreamOptionsCfg stream_options 注入配置
type StreamOptionsCfg struct {
	Disabled bool     `yaml:"disabled"`  // 是否禁用 stream_options 注入
	Paths    []string `yaml:"paths"`     // 需要注入的路径列表，为空时使用默认 OpenAI 兼容路径
}

// ExporterCfg 数据导出配置
type ExporterCfg struct {
	Type       string              `yaml:"type"`       // redis | prometheus
	Redis      ExporterRedisCfg    `yaml:"redis"`      // Redis exporter 配置
	Prometheus ExporterPromCfg     `yaml:"prometheus"` // Prometheus exporter 配置
}

// ExporterRedisCfg Redis exporter 配置
type ExporterRedisCfg struct {
	StreamKey    string `yaml:"stream_key"`    // Redis Stream Key
	StreamMaxLen int64  `yaml:"stream_max_len"` // Redis Stream 最大长度
}

// ExporterPromCfg Prometheus exporter 配置
type ExporterPromCfg struct {
	MetricsAddr string `yaml:"metrics_addr"` // HTTP /metrics 监听地址，如 ":9090"
}

// Load 从 YAML 配置文件加载配置
func Load(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	// 设置默认值
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "localhost:6379"
	}
	if cfg.Aggr.WindowDuration == 0 {
		cfg.Aggr.WindowDuration = 30 * time.Second
	}
	if cfg.Aggr.ChannelSize == 0 {
		cfg.Aggr.ChannelSize = 10000
	}
	// 聚合默认开启
	// （bool 零值为 false，这里不做特殊处理，用户显式配置即可）
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.GRPCMaxRecvSize == 0 {
		cfg.GRPCMaxRecvSize = 64 << 20 // 64MB
	}
	// MaskLen 0 表示全部掩码
	// StreamOptions.Disabled 默认 false（启用注入）
	if len(cfg.StreamOptions.Paths) == 0 {
		cfg.StreamOptions.Paths = []string{
			"/v1/chat/completions",
			"/v1/completions",
			"/v1/embeddings",
		}
	}
	if cfg.Exporter.Type == "" {
		cfg.Exporter.Type = "redis"
	}
	if cfg.Exporter.Redis.StreamKey == "" {
		cfg.Exporter.Redis.StreamKey = "llm:usage"
	}
	if cfg.Exporter.Redis.StreamMaxLen == 0 {
		cfg.Exporter.Redis.StreamMaxLen = 10000
	}
	if cfg.Exporter.Prometheus.MetricsAddr == "" {
		cfg.Exporter.Prometheus.MetricsAddr = ":9090"
	}

	return &cfg, nil
}

// String 安全打印配置（隐藏密码）
func (c *Config) String() string {
	password := c.Redis.Password
	if len(password) > 0 {
		if len(password) > 4 {
			password = password[:2] + "****" + password[len(password)-2:]
		} else {
			password = "****"
		}
	}

	return fmt.Sprintf(`Configuration:
  Redis:
    Addr: %s
    Password: %s
    DB: %d
  Aggregator:
    WindowDuration: %v
    Enabled: %v
    ChannelSize: %d
  GRPCMaxRecvSize: %d
  MaskLen: %d
  StreamOptions:
    Disabled: %v
    Paths: %v
  Exporter:
    Type: %s
    Redis:
      StreamKey: %s
      StreamMaxLen: %d
    Prometheus:
      MetricsAddr: %s
  Log:
    Level: %s`,
		c.Redis.Addr,
		password,
		c.Redis.DB,
		c.Aggr.WindowDuration,
		c.Aggr.Enabled,
		c.Aggr.ChannelSize,
		c.GRPCMaxRecvSize,
		c.MaskLen,
		c.StreamOptions.Disabled,
		c.StreamOptions.Paths,
		c.Exporter.Type,
		c.Exporter.Redis.StreamKey,
		c.Exporter.Redis.StreamMaxLen,
		c.Exporter.Prometheus.MetricsAddr,
		c.Log.Level,
	)
}
