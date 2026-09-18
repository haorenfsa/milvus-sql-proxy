package pkg

import "github.com/milvus-io/milvus-sdk-go/v2/entity"

// MilvusSchema includes collection fields and its shard count.
type MilvusSchema struct {
	*entity.Schema
	ShardNum int32
}
