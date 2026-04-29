package resp

type Responser interface {
	GetModel() string
	GetCachedToken() int64
	GetInputToken() int64
	GetOutputToken() int64
	String() string
}
