package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"tokenusage/config"
)

// Client Redis 客户端封装
type Client struct {
	client *redis.Client
}

// NewClient 创建 Redis 客户端
func NewClient(cfg *config.Config) (*Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:            cfg.Redis.Addr,
		Password:        cfg.Redis.Password,
		DB:              cfg.Redis.DB,
		MaxRetries:      3,
		MinRetryBackoff: 100 * time.Millisecond,
		MaxRetryBackoff: 500 * time.Millisecond,
	})

	// 测试连接
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("connect to redis: %w", err)
	}

	return &Client{client: client}, nil
}

// XAdd 向 Redis Stream 添加消息
func (c *Client) XAdd(ctx context.Context, streamKey string, fields map[string]interface{}, maxLen int64) error {
	return c.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: fields,
		MaxLen: maxLen,
		Approx: true, // 使用近似裁剪，提高性能
	}).Err()
}

// Close 关闭连接
func (c *Client) Close() error {
	return c.client.Close()
}