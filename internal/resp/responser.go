package resp

// Responser 定义响应体解析接口（保留用于扩展）
type Responser interface {
	GetModel() string
	GetCachedToken() int64
	GetInputToken() int64
	GetOutputToken() int64
	String() string
}
