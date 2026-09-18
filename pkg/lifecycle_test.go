package pkg

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func (m *operationMock) CreateDatabase(_ context.Context, name string, _ ...client.CreateDatabaseOption) error {
	m.calls = append(m.calls, "create db "+name)
	return m.fail
}
func (m *operationMock) DropDatabase(_ context.Context, name string, _ ...client.DropDatabaseOption) error {
	m.calls = append(m.calls, "drop db "+name)
	return m.fail
}
func (m *operationMock) ListDatabases(context.Context) ([]entity.Database, error) {
	return []entity.Database{{Name: "default"}}, m.fail
}
func (m *operationMock) ListCollections(context.Context) ([]*entity.Collection, error) {
	return []*entity.Collection{{Name: "items"}}, m.fail
}
func (m *operationMock) LoadCollection(_ context.Context, name string, async bool, _ ...client.LoadCollectionOption) error {
	if async {
		return errors.New("expected synchronous load")
	}
	m.calls = append(m.calls, "load "+name)
	return m.fail
}
func (m *operationMock) ReleaseCollection(_ context.Context, name string, _ ...client.ReleaseCollectionOption) error {
	m.calls = append(m.calls, "release "+name)
	return m.fail
}
func (m *operationMock) Flush(_ context.Context, name string, async bool, _ ...client.FlushOption) error {
	if async {
		return errors.New("expected synchronous flush")
	}
	m.calls = append(m.calls, "flush "+name)
	return m.fail
}
func (m *operationMock) CreateIndex(_ context.Context, table, field string, idx entity.Index, async bool, _ ...client.IndexOption) error {
	if async {
		return errors.New("expected synchronous index")
	}
	m.calls = append(m.calls, "index "+table+" "+field+" "+idx.Name()+" "+string(idx.IndexType())+" "+idx.Params()["metric_type"])
	return m.fail
}
func (m *operationMock) DropIndex(_ context.Context, table, field string, _ ...client.IndexOption) error {
	m.calls = append(m.calls, "drop index "+table)
	return m.fail
}
func (m *operationMock) DescribeIndex(context.Context, string, string, ...client.IndexOption) ([]entity.Index, error) {
	return []entity.Index{entity.NewGenericIndex("vecidx", entity.HNSW, map[string]string{"metric_type": "COSINE"})}, m.fail
}
func (m *operationMock) CreatePartition(_ context.Context, table, partition string, _ ...client.CreatePartitionOption) error {
	m.calls = append(m.calls, "create partition "+table+" "+partition)
	return m.fail
}
func (m *operationMock) DropPartition(_ context.Context, table, partition string, _ ...client.DropPartitionOption) error {
	m.calls = append(m.calls, "drop partition "+table+" "+partition)
	return m.fail
}
func (m *operationMock) LoadPartitions(_ context.Context, table string, parts []string, async bool, _ ...client.LoadPartitionsOption) error {
	m.calls = append(m.calls, "load partition "+table+" "+parts[0])
	return m.fail
}
func (m *operationMock) ReleasePartitions(_ context.Context, table string, parts []string, _ ...client.ReleasePartitionsOption) error {
	m.calls = append(m.calls, "release partition "+table+" "+parts[0])
	return m.fail
}
func (m *operationMock) ShowPartitions(context.Context, string) ([]*entity.Partition, error) {
	return []*entity.Partition{{Name: "_default"}}, m.fail
}
func (m *operationMock) Search(_ context.Context, table string, _ []string, filter string, fields []string, vecs []entity.Vector, field string, metric entity.MetricType, k int, params entity.SearchParam, opts ...client.SearchQueryOptionFunc) ([]client.SearchResult, error) {
	m.queryFilter = filter
	m.calls = append(m.calls, "search")
	if table != "items" || field != "vec" || metric != entity.COSINE || len(vecs) != 1 || k != 2 || params.Params()["ef"] != int64(64) {
		return nil, errors.New("unexpected search arguments")
	}
	return []client.SearchResult{{ResultCount: 1, IDs: entity.NewColumnInt64("id", []int64{7}), Scores: []float32{0.75}, Fields: client.ResultSet{entity.NewColumnVarChar("name", []string{"one"})}}}, m.fail
}
func TestLifecycleRouting(t *testing.T) {
	ctx := context.Background()
	m := &operationMock{schema: testSchema()}
	c := NewSession(ctx, m)
	cases := []struct{ sql, call string }{
		{"CREATE DATABASE demo", "create db demo"}, {"DROP DATABASE demo", "drop db demo"}, {"LOAD TABLE items", "load items"}, {"RELEASE TABLE items", "release items"}, {"FLUSH TABLE items", "flush items"}, {"CREATE INDEX idx ON items (vec) USING FLAT", "index items vec idx FLAT L2"}, {"CREATE INDEX idx ON items (vec) USING HNSW WITH (metric_type='COSINE', M=16, efConstruction=100)", "index items vec idx HNSW COSINE"}, {"CREATE INDEX idx ON items (vec) USING IVF_FLAT", "index items vec idx IVF_FLAT L2"}, {"CREATE INDEX idx ON items (name) USING INVERTED", "index items name idx INVERTED "}, {"DROP INDEX idx ON items", "drop index items"}, {"CREATE PARTITION p ON items", "create partition items p"}, {"DROP PARTITION p ON items", "drop partition items p"}, {"LOAD PARTITION p ON items", "load partition items p"}, {"RELEASE PARTITION p ON items", "release partition items p"},
	}
	for _, tc := range cases {
		t.Run(tc.sql, func(t *testing.T) {
			m.calls = nil
			if _, e := c.Describe(ctx, tc.sql); e != nil {
				t.Fatal(e)
			}
			if len(m.calls) != 0 {
				t.Fatal("Describe mutated upstream", m.calls)
			}
			if _, e := c.Execute(ctx, tc.sql); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(m.calls, []string{tc.call}) {
				t.Fatal(m.calls)
			}
			m.fail = errors.New("upstream")
			if _, e := c.Execute(ctx, tc.sql); e == nil {
				t.Fatal("upstream error lost")
			}
			m.fail = nil
		})
	}
	for _, q := range []string{"SHOW DATABASES", "SHOW TABLES", "SHOW PARTITIONS FROM items", "DESCRIBE items"} {
		r, e := c.Execute(ctx, q)
		if e != nil || r.Resultset == nil || len(r.Values) == 0 {
			t.Fatal(q, r, e)
		}
		d, e := c.Describe(ctx, q)
		if e != nil || len(d.Fields) != len(r.Fields) {
			t.Fatal(q, d, e)
		}
		for i := range d.Fields {
			if pgOID(d.Fields[i]) != pgOID(r.Fields[i]) {
				t.Fatal("metadata mismatch", q)
			}
		}
		m.fail = errors.New("upstream")
		if _, e := c.Execute(ctx, q); e == nil {
			t.Fatal("upstream error lost")
		}
		m.fail = nil
	}
	for _, q := range []string{"CREATE INDEX idx ON items (vec) USING FLAT WITH (metric_type='BOGUS')", "CREATE INDEX idx ON items (vec) USING FLAT WITH (bogus=1)", "CREATE INDEX idx ON items (vec) USING FLAT WITH (nlist)", "CREATE INDEX idx ON items (vec) USING FLAT WITH (nlist=1,nlist=2)"} {
		if _, e := c.Execute(ctx, q); e == nil {
			t.Fatal(q)
		}
	}
}
func TestVectorSearch(t *testing.T) {
	m := &operationMock{schema: testSchema()}
	c := NewSession(context.Background(), m)
	r, e := c.Execute(context.Background(), "SELECT id,name,_distance FROM items WHERE vec LIKE json_vector('[1,2,3]') AND enabled=true LIMIT 2")
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(r.Values, [][]interface{}{{int64(7), "one", float32(.75)}}) || m.queryFilter != "enabled == true" {
		t.Fatal(r.Values, m.queryFilter)
	}
	for _, q := range []string{"SELECT id FROM items WHERE vec LIKE json_vector('[1,2,3]') OR id=1 LIMIT 2", "SELECT id FROM items WHERE NOT (vec LIKE json_vector('[1,2,3]')) LIMIT 2", "SELECT id FROM items WHERE vec LIKE json_vector('[1,2,3]') AND vec LIKE json_vector('[1,2,3]') LIMIT 2", "SELECT id FROM items WHERE name LIKE json_vector('[1,2,3]') LIMIT 2", "SELECT count(*) FROM items WHERE vec LIKE json_vector('[1,2,3]') LIMIT 2", "SELECT id FROM items WHERE vec LIKE json_vector('[1]') LIMIT 2"} {
		if _, e := c.Execute(context.Background(), q); e == nil {
			t.Fatal(q)
		}
	}
	p := &querySearchParams{values: map[string]interface{}{}}
	p.AddRadius(1)
	p.AddRangeFilter(2)
	if p.Params()["radius"] != float64(1) {
		t.Fatal(p)
	}
}
