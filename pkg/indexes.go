package pkg

import (
	"fmt"
	"github.com/milvus-io/milvus-proto/go-api/v2/commonpb"
	"github.com/milvus-io/milvus-proto/go-api/v2/milvuspb"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// SDK v2.3 DescribeIndex validates a nonempty field name, preventing an all-index
// query. Its exported gRPC service uses the same database/auth interceptors.
func (c *ClientConn) listIndexes(table string) ([]entity.Index, error) {
	upstream, ok := c.upstream.(*client.GrpcClient)
	if !ok {
		return nil, fmt.Errorf("index listing requires the Milvus gRPC client")
	}
	resp, err := upstream.Service.DescribeIndex(c.ctx, &milvuspb.DescribeIndexRequest{CollectionName: table})
	if err != nil {
		return nil, err
	}
	status := resp.GetStatus()
	if status.GetErrorCode() == commonpb.ErrorCode_IndexNotExist {
		return nil, nil
	}
	if status == nil || status.GetErrorCode() != commonpb.ErrorCode_Success || status.GetCode() != 0 {
		return nil, fmt.Errorf("describe indexes: %s", status.GetReason())
	}
	indexes := []entity.Index{}
	for _, d := range resp.GetIndexDescriptions() {
		params := entity.KvPairsMap(d.Params)
		indexes = append(indexes, entity.NewGenericIndex(d.IndexName, entity.IndexType(params["index_type"]), params))
	}
	return indexes, nil
}
