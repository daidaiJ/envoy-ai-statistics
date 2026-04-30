package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 聚合器配置
type Config struct {
	Redis  RedisConfig  `yaml:"redis"`
	Aggr   AggrConfig   `yaml:"aggregator"`
	Log    LogConfig    `yaml:"log"`
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
	StreamKey      string        `yaml:"stream_key"`       // Redis Stream Key
	WindowDuration time.Duration `yaml:"window_duration"`  // 聚合窗口时长，如 "30s"
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
	if cfg.Aggr.StreamKey == "" {
		cfg.Aggr.StreamKey = "llm:usage"
	}
	if cfg.Aggr.WindowDuration == 0 {
		cfg.Aggr.WindowDuration = 30 * time.Second
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
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
    StreamKey: %s
    WindowDuration: %v
  Log:
    Level: %s`,
		c.Redis.Addr,
		password,
		c.Redis.DB,
		c.Aggr.StreamKey,
		c.Aggr.WindowDuration,
		c.Log.Level,
	)
}