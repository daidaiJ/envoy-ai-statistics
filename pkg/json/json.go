package json

import jsoniter "github.com/json-iterator/go"

var (
	Marshal   = jsoniter.Marshal
	Unmarshal = jsoniter.Unmarshal
)