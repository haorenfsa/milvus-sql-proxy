module github.com/haorenfsa/milvus-sql-proxy

go 1.18

require (
	github.com/flike/kingshard v0.0.0-20200829024017-f17b39394746
	github.com/milvus-io/milvus-sdk-go/v2 v2.3.3
	github.com/pkg/errors v0.9.1
	github.com/xwb1989/sqlparser v0.0.0-20180606152119-120387863bf2
	gopkg.in/yaml.v2 v2.2.5
)

require (
	github.com/cockroachdb/errors v1.9.1 // indirect
	github.com/cockroachdb/logtags v0.0.0-20211118104740-dabe8e521a4f // indirect
	github.com/cockroachdb/redact v1.1.3 // indirect
	github.com/getsentry/sentry-go v0.12.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0 // indirect
	github.com/kr/pretty v0.3.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/milvus-io/milvus-proto/go-api/v2 v2.3.3 // indirect
	github.com/rogpeppe/go-internal v1.8.1 // indirect
	github.com/tidwall/gjson v1.14.4 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	google.golang.org/genproto v0.0.0-20220503193339-ba3ae3f07e29 // indirect
	google.golang.org/grpc v1.48.0 // indirect
	google.golang.org/protobuf v1.30.0 // indirect
)

replace github.com/xwb1989/sqlparser => github.com/haorenfsa/sqlparser v0.1.0
