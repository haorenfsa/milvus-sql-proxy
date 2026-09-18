package pkg

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-proto/go-api/v2/commonpb"
	"github.com/milvus-io/milvus-proto/go-api/v2/milvuspb"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"google.golang.org/grpc"
)

func TestScalarSerialization(t *testing.T) {
	for _, tc := range []struct {
		value interface{}
		text  string
	}{
		{nil, "NULL"}, {true, "true"}, {false, "false"}, {int8(-8), "-8"}, {int16(-16), "-16"}, {int32(-32), "-32"}, {int64(-64), "-64"}, {int(-1), "-1"}, {uint8(8), "8"}, {uint16(16), "16"}, {uint32(32), "32"}, {uint64(64), "64"}, {uint(1), "1"}, {float32(1.5), "1.5"}, {float64(2.5), "2.5"}, {[]float32{1, 2}, "[1,2]"}, {[]byte("bytes"), "bytes"}, {"str", "str"},
	} {
		b, e := formatValue(tc.value)
		if e != nil || string(b) != tc.text {
			t.Fatal(tc, b, e)
		}
		f := &mysql.Field{}
		if e = formatField(f, tc.value); e != nil {
			t.Fatal(tc, e)
		}
	}
	if _, e := formatValue(struct{}{}); e == nil {
		t.Fatal("accepted unsupported value")
	}
	if e := formatField(&mysql.Field{}, struct{}{}); e == nil {
		t.Fatal("accepted unsupported field")
	}
	c := NewSession(context.Background(), nil)
	if _, e := c.buildResultset(nil, []string{"a"}, [][]interface{}{{1, 2}}); e == nil {
		t.Fatal("row size accepted")
	}
	if _, e := c.buildResultset([]*mysql.Field{{}}, []string{"a", "b"}, nil); e == nil {
		t.Fatal("schema size accepted")
	}
	if _, e := c.buildResultset(nil, []string{"a"}, [][]interface{}{{struct{}{}}}); e == nil {
		t.Fatal("value accepted")
	}
	for _, v := range []interface{}{nil, []byte("x"), true, false, int(1), uint64(3), float64(1.25)} {
		if _, e := sqlLiteral(v); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := sqlLiteral(struct{}{}); e == nil {
		t.Fatal("parameter accepted")
	}
}
func TestParameterInference(t *testing.T) {
	c := NewSession(context.Background(), &operationMock{schema: testSchema()})
	for _, tc := range []struct {
		q    string
		want []uint32
	}{
		{"SELECT id FROM items WHERE $1=id", []uint32{20}},
		{"SELECT id FROM items WHERE id IN ($1,$2) LIMIT $3 OFFSET $4", []uint32{20, 20, 20, 20}},
		{"DELETE FROM items WHERE enabled=$1", []uint32{16}},
		{"INSERT INTO items VALUES ($1,$2,$3,$4,$5,json_vector($6))", []uint32{20, 25, 16, 701, 25, 25}},
		{"INSERT INTO items(score,id) VALUES ($1,$2)", []uint32{701, 20}},
		{"SELECT $1", []uint32{25}},
		{"SHOW $1", []uint32{25}},
	} {
		oids, e := c.parameterOIDs(context.Background(), tc.q, make([]uint32, len(tc.want)))
		if e != nil || !reflect.DeepEqual(oids, tc.want) {
			t.Fatal(tc.q, oids, e)
		}
	}
	if v, e := c.parameterOIDs(context.Background(), "SELECT id FROM items WHERE id=$1", []uint32{23}); e != nil || v[0] != 23 {
		t.Fatal(v, e)
	}
	c.upstream = &operationMock{fail: errors.New("schema unavailable")}
	if _, e := c.parameterOIDs(context.Background(), "SELECT id FROM items WHERE id=$1", []uint32{0}); e == nil {
		t.Fatal("schema failure ignored")
	}
}
func TestParameterWireTypes(t *testing.T) {
	for _, tc := range []struct {
		oid  uint32
		text string
		want interface{}
	}{{16, "true", true}, {20, "-5", int64(-5)}, {701, "1.5", float64(1.5)}, {25, "str", "str"}} {
		v, e := pgParameter([]byte(tc.text), tc.oid, 0)
		if e != nil || v != tc.want {
			t.Fatal(v, e)
		}
	}
	small := []byte{0xff, 0xfe}
	if v, e := pgParameter(small, 21, 1); e != nil || v != int16(-2) {
		t.Fatal(v, e)
	}
	real := make([]byte, 4)
	binary.BigEndian.PutUint32(real, math.Float32bits(1.5))
	if v, e := pgParameter(real, 700, 1); e != nil || v != float32(1.5) {
		t.Fatal(v, e)
	}
	if _, e := pgParameter([]byte{2}, 16, 1); e == nil {
		t.Fatal("bad bool accepted")
	}
	if _, e := pgParameter([]byte("x"), 9999, 1); e == nil {
		t.Fatal("unsupported OID accepted")
	}
	if v, e := pgValue(nil, 25, 1); e != nil || v != nil {
		t.Fatal(v, e)
	}
	for _, oid := range []uint32{16, 20, 701} {
		if _, e := pgValue("not numeric", oid, 1); e == nil {
			t.Fatal(oid)
		}
	}
	if formatAt([]int16{0, 1}, 3) != 0 {
		t.Fatal("format overflow")
	}
}

type indexService struct {
	milvuspb.MilvusServiceClient
	response *milvuspb.DescribeIndexResponse
	err      error
	request  *milvuspb.DescribeIndexRequest
}

func (s *indexService) DescribeIndex(_ context.Context, r *milvuspb.DescribeIndexRequest, _ ...grpc.CallOption) (*milvuspb.DescribeIndexResponse, error) {
	s.request = r
	return s.response, s.err
}
func TestIndexResponseFiltering(t *testing.T) {
	service := &indexService{response: &milvuspb.DescribeIndexResponse{Status: &commonpb.Status{}, IndexDescriptions: []*milvuspb.IndexDescription{{IndexName: "scalar", FieldName: "name", Params: entity.MapKvPairs(map[string]string{"index_type": "INVERTED"})}, {IndexName: "first", FieldName: "v1", Params: entity.MapKvPairs(map[string]string{"index_type": "FLAT", "metric_type": "L2"})}, {IndexName: "second", FieldName: "v2", Params: entity.MapKvPairs(map[string]string{"index_type": "HNSW", "metric_type": "IP"})}}}}
	c := NewSession(context.Background(), &client.GrpcClient{Service: service})
	indexes, e := c.vectorIndexes("items", "v2")
	if e != nil || len(indexes) != 1 || indexes[0].Params()["metric_type"] != "IP" {
		t.Fatal(indexes, e)
	}
	if service.request.FieldName != "v2" {
		t.Fatal(service.request)
	}
	all, e := c.listIndexes("items")
	if e != nil || len(all) != 3 {
		t.Fatal(all, e)
	}
	if _, e := c.Execute(context.Background(), "SHOW INDEXES FROM items"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Describe(context.Background(), "SHOW INDEXES FROM items"); e != nil {
		t.Fatal(e)
	}
	service.response.Status = &commonpb.Status{ErrorCode: commonpb.ErrorCode_IndexNotExist}
	all, e = c.listIndexes("items")
	if e != nil || len(all) != 0 {
		t.Fatal(all, e)
	}
	service.response.Status = &commonpb.Status{ErrorCode: commonpb.ErrorCode_UnexpectedError, Reason: "failure"}
	if _, e = c.listIndexes("items"); e == nil {
		t.Fatal("error lost")
	}
	if _, e = c.vectorIndexes("items", "v2"); e == nil {
		t.Fatal("error lost")
	}
	service.err = errors.New("transport failed")
	if _, e = c.listIndexes("items"); e == nil {
		t.Fatal("error lost")
	}
	if _, e = c.vectorIndexes("items", "v2"); e == nil {
		t.Fatal("error lost")
	}
}
func TestIndexOptions(t *testing.T) {
	for _, p := range []map[string]string{{"index_type": "HNSW", "M": "oops", "efConstruction": "100"}, {"index_type": "HNSW", "M": "16", "efConstruction": "oops"}, {"index_type": "IVF_FLAT", "nlist": "oops"}, {"index_type": "FLAT", "nlist": "100"}, {"index_type": "unsupported"}} {
		if _, e := buildIndex(p); e == nil {
			t.Fatal(p)
		}
	}
	if _, e := buildIndex(map[string]string{"index_type": "AUTOINDEX", "metric_type": "L2"}); e != nil {
		t.Fatal(e)
	}
}
func TestConfigFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if e := os.WriteFile(path, []byte("mode: both\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := ParseConfigFile(path); e != nil {
		t.Fatal(e)
	}
	if _, e := ParseConfigFile(path + ".missing"); e == nil {
		t.Fatal("missing config accepted")
	}
	if _, e := ParseConfigData([]byte("[bad")); e == nil {
		t.Fatal("invalid yaml accepted")
	}
}

func TestIndexAndSchemaFailures(t *testing.T) {
	m := &operationMock{schema: testSchema()}
	c := NewSession(context.Background(), m)
	for _, q := range []string{"", "SELECT (", "SELECT COUNT(*) FROM items LIMIT 1", "SELECT id FROM items LIMIT 16385", "SELECT unknown FROM items", "CREATE TABLE bad (id bigint primary key, _distance bigint, v vector(3))", "CREATE TABLE bad (id bigint primary key, v vector)", "CREATE TABLE bad (id bigint primary key, name varchar, v vector(3))", "CREATE TABLE bad (id bigint primary key, x bigint auto_increment, v vector(3))", "CREATE TABLE bad (id bigint primary key, x bigint unique, v vector(3))", "DELETE FROM items i WHERE id=1", "DELETE FROM items,other WHERE id=1", "DELETE FROM items WHERE id IS NULL"} {
		if _, e := c.Execute(context.Background(), q); e == nil {
			t.Fatal(q)
		}
	}
	if _, e := c.Execute(context.Background(), "SELECT COUNT(*) FROM items"); e != nil {
		t.Fatal(e)
	}
	m.schema = nil
	if _, e := c.GetCollectinSchema("items"); e == nil {
		t.Fatal("missing schema accepted")
	}
	m.schema = testSchema()
	m.fail = errors.New("denied")
	if e := c.handleUseDB("other", nil); e == nil {
		t.Fatal("database error ignored")
	}
	if _, e := c.Execute(context.Background(), "CREATE TABLE t (id bigint primary key,v vector(3))"); e == nil {
		t.Fatal("create error ignored")
	}
	if _, e := emptyColumn(&entity.Field{DataType: entity.FieldTypeFloatVector}); e == nil {
		t.Fatal("dimension missing")
	}
	if _, e := emptyColumn(&entity.Field{DataType: entity.FieldTypeBinaryVector}); e == nil {
		t.Fatal("unsupported type accepted")
	}
}
func TestMySQLResultMetadata(t *testing.T) {
	c := NewSession(context.Background(), nil)
	r, e := c.buildResultset(nil, []string{"flag", "vec"}, [][]interface{}{{false, []float32{1, 2}}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = mysqlResult(&mysql.Result{Resultset: r}, false); e != nil {
		t.Fatal(e)
	}
	empty := newEmptyResultset([]string{"id"})
	empty.Fields[0] = resultField("id", entity.FieldTypeInt64)
	mr, e := mysqlResult(&mysql.Result{Resultset: empty}, true)
	if e != nil || mr.Fields[0].Type != mysql.MYSQL_TYPE_LONGLONG {
		t.Fatal(mr, e)
	}
	h := &mysqlHandler{ctx: context.Background(), timeout: time.Second, session: NewSession(context.Background(), &operationMock{schema: testSchema()})}
	if _, e := h.HandleFieldList("items", ""); e == nil {
		t.Fatal("field list accepted")
	}
	if e := h.HandleOtherCommand(0, nil); e == nil {
		t.Fatal("command accepted")
	}
	if _, _, _, e := h.HandleStmtPrepare("SELECT '"); e == nil {
		t.Fatal("bad prepare")
	}
	if _, e := h.HandleStmtExecute(nil, "SELECT id FROM items WHERE id=?", nil); e == nil {
		t.Fatal("missing parameter accepted")
	}
}
func TestPostgresCommandTags(t *testing.T) {
	for _, tc := range []struct{ q, want string }{{"INSERT INTO x VALUES (1)", "INSERT 0 2"}, {"UPSERT INTO x VALUES (1)", "INSERT 0 2"}, {"DELETE FROM x WHERE id=1", "DELETE 2"}, {"CREATE TABLE x", "CREATE TABLE"}, {"DROP DATABASE x", "DROP DATABASE"}, {"LOAD TABLE x", "LOAD"}, {"", ""}} {
		if v := pgTag(tc.q, &mysql.Result{AffectedRows: 2}); v != tc.want {
			t.Fatal(v, tc.want)
		}
	}
}
func TestNormalizeTypesQuotedValues(t *testing.T) {
	q := "CREATE TABLE `a` (`id` BIGINT PRIMARY KEY, name VARCHAR(9) COMMENT 'bool \\' x', v VECTOR(3), flag BOOLEAN)"
	got := normalizeTypes(q)
	if !strings.Contains(got, "flag tinyint(1)") || !strings.Contains(got, "COMMENT 'bool \\' x'") {
		t.Fatal(got)
	}
	if normalizeTypes("SELECT 'boolean'") != "SELECT 'boolean'" {
		t.Fatal("rewrote non-DDL")
	}
}
