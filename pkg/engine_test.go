package pkg

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/xwb1989/sqlparser"
)

func testSchema() *entity.Schema {
	return &entity.Schema{CollectionName: "items", Fields: []*entity.Field{{Name: "id", DataType: entity.FieldTypeInt64, PrimaryKey: true}, {Name: "name", DataType: entity.FieldTypeVarChar, TypeParams: map[string]string{"max_length": "16"}}, {Name: "enabled", DataType: entity.FieldTypeBool}, {Name: "score", DataType: entity.FieldTypeDouble}, {Name: "meta", DataType: entity.FieldTypeJSON}, {Name: "vec", DataType: entity.FieldTypeFloatVector, TypeParams: map[string]string{"dim": "3"}}}}
}
func insertAST(t *testing.T, q string) *sqlparser.Insert {
	t.Helper()
	s, e := sqlparser.Parse(q)
	if e != nil {
		t.Fatal(e)
	}
	return s.(*sqlparser.Insert)
}
func TestInsertValidation(t *testing.T) {
	c := NewSession(context.Background(), nil)
	schema := testSchema()
	columns, e := c.FillInsertColumns(schema, insertAST(t, `insert into items values (-5,'one',true,-1.5,'{"x":1}',json_vector('[1,2,3]')), (6,'two',false,2.5,'{}',json_vector('[3,2,1]'))`))
	if e != nil {
		t.Fatal(e)
	}
	if len(columns) != 6 || columns[0].Len() != 2 {
		t.Fatal(columns)
	}
	v, _ := columns[0].Get(0)
	if v != int64(-5) {
		t.Fatal(v)
	}
	v, _ = columns[2].Get(0)
	if v != true {
		t.Fatal(v)
	}
	for _, q := range []string{
		`insert into items values (1,'x')`,
		`insert into items values (1,'x',true,1,'{}',json_vector('[1,2]'))`,
		`insert into items values (1,'x',true,1,'{}',json_vector('[null,2,3]'))`,
		`insert into items values (1,'x',true,1,'{}',json_vector('["1",2,3]'))`,
		`insert into items values (1,'x',true,1,'not json',json_vector('[1,2,3]'))`,
		`insert into items values (1,'x',true,abs(1),'{}',json_vector('[1,2,3]'))`,
		`insert into items values (1,'x',null,1,'{}',json_vector('[1,2,3]'))`,
		`insert into items values (9223372036854775808,'x',true,1,'{}',json_vector('[1,2,3]'))`,
		`insert into items values (1,'this string is too long',true,1,'{}',json_vector('[1,2,3]'))`,
		`insert into items values (1,42,true,1,'{}',json_vector('[1,2,3]'))`,
		`insert into items(id,id) values (1,2)`, `insert into items(nope) values (1)`, `insert into items(id) values (1)`, `insert into items select * from x`,
		`insert into items values (1,'x',true,1,'{}',unknown('[1,2,3]'))`,
		`insert into items values (1,'x',true,1,'{}',json_vector('[1,2,3]','x'))`,
	} {
		t.Run(q, func(t *testing.T) {
			if _, e := c.FillInsertColumns(schema, insertAST(t, q)); e == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	schema.Fields[0].AutoID = true
	if _, e := c.FillInsertColumns(schema, insertAST(t, `insert into items(name,enabled,score,meta,vec) values ('x',true,1,'{}',json_vector('[1,2,3]'))`)); e != nil {
		t.Fatal(e)
	}
	if _, e := c.FillInsertColumns(schema, insertAST(t, `replace into items(name,enabled,score,meta,vec) values ('x',true,1,'{}',json_vector('[1,2,3]'))`)); e == nil {
		t.Fatal("autoID upsert accepted")
	}
}
func TestFieldTypes(t *testing.T) {
	for _, tc := range []struct {
		kind      entity.FieldType
		good, bad string
	}{
		{entity.FieldTypeInt8, "-128", "128"}, {entity.FieldTypeInt16, "32767", "32768"}, {entity.FieldTypeInt32, "2147483647", "2147483648"}, {entity.FieldTypeInt64, "-9223372036854775808", "9223372036854775808"}, {entity.FieldTypeFloat, "1.25", "'NaN'"}, {entity.FieldTypeDouble, "2.5", "'Inf'"}, {entity.FieldTypeBool, "0", "'invalid'"},
	} {
		f := &entity.Field{Name: "f", DataType: tc.kind}
		col, e := emptyColumn(f)
		if e != nil {
			t.Fatal(e)
		}
		for i, literal := range []string{tc.good, tc.bad} {
			stmt := insertAST(t, "insert into x values ("+literal+")")
			v, e := fieldValue(f, stmt.Rows.(sqlparser.Values)[0][0])
			if i == 0 {
				if e != nil {
					t.Fatal(e)
				}
				if e = col.AppendValue(v); e != nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatalf("accepted %s as %v", literal, tc.kind)
			}
		}
	}
}
func TestDDLValidation(t *testing.T) {
	for _, q := range []string{`create table t (id bigint primary key, v vector(3))`, `create table t (id bigint auto_increment primary key, name varchar(16), flag boolean, meta json, v vector(3))`} {
		s, e := sqlparser.ParseStrictDDL(normalizeTypes(q))
		if e != nil {
			t.Fatal(e)
		}
		schema, e := DDLToMilvusSchema(s.(*sqlparser.DDL))
		if e != nil {
			t.Fatal(e)
		}
		if schema.CollectionName != "t" {
			t.Fatal(schema)
		}
	}
	for _, q := range []string{`create table t (id int primary key, v vector(3))`, `create table t (id bigint, v vector(3))`, `create table t (id bigint primary key, v vector(0))`, `create table t (id bigint primary key, v vector(32769))`, `create table t (id bigint primary key, name varchar(0), v vector(3))`, `create table t (id bigint primary key, id bigint, v vector(3))`, `create table t (id bigint primary key, v vector(3), x bigint default 1)`, `create table t (id bigint primary key)`, `create table t (id bigint primary key, v vector(3), primary key (id))`, `create table t (id varchar(16) auto_increment primary key, v vector(3))`} {
		s, e := sqlparser.ParseStrictDDL(normalizeTypes(q))
		if e != nil {
			continue
		}
		if _, e := DDLToMilvusSchema(s.(*sqlparser.DDL)); e == nil {
			t.Fatalf("accepted %s", q)
		}
	}
}
func TestDangerousSQLRejectedBeforeRPC(t *testing.T) {
	c := NewSession(context.Background(), nil)
	for _, q := range []string{"DROP VIEW items", "DROP TABLE items CASCADE", "DROP TABLE items; DROP TABLE other", "DROP DATABASE IF EXISTS db", "CREATE TABLE IF NOT EXISTS items (id bigint primary key,v vector(3))", "SHOW TABLES FROM other LIKE 'x%'", "SHOW FULL TABLES", "BEGIN", "UPDATE items SET id=2"} {
		if _, e := c.Execute(context.Background(), q); e == nil {
			t.Fatalf("accepted %s", q)
		}
	}
}

type operationMock struct {
	client.Client
	schema      *entity.Schema
	calls       []string
	columns     []entity.Column
	fail        error
	db          string
	queryFilter string
}

func (m *operationMock) Close() error                                     { return nil }
func (m *operationMock) UsingDatabase(_ context.Context, db string) error { m.db = db; return m.fail }
func (m *operationMock) DescribeCollection(context.Context, string) (*entity.Collection, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	return &entity.Collection{Schema: m.schema}, nil
}
func (m *operationMock) CreateCollection(_ context.Context, s *entity.Schema, _ int32, _ ...client.CreateCollectionOption) error {
	m.calls = append(m.calls, "create")
	m.schema = s
	return m.fail
}
func (m *operationMock) DropCollection(context.Context, string, ...client.DropCollectionOption) error {
	m.calls = append(m.calls, "drop")
	return m.fail
}
func (m *operationMock) Insert(_ context.Context, _, _ string, cols ...entity.Column) (entity.Column, error) {
	m.calls = append(m.calls, "insert")
	m.columns = cols
	return entity.NewColumnInt64("id", []int64{7}), m.fail
}
func (m *operationMock) Upsert(_ context.Context, _, _ string, cols ...entity.Column) (entity.Column, error) {
	m.calls = append(m.calls, "upsert")
	m.columns = cols
	return entity.NewColumnInt64("id", []int64{7}), m.fail
}
func (m *operationMock) Delete(_ context.Context, _, _, filter string) error {
	m.calls = append(m.calls, "delete")
	m.queryFilter = filter
	return m.fail
}
func (m *operationMock) Query(_ context.Context, _ string, _ []string, filter string, fields []string, _ ...client.SearchQueryOptionFunc) (client.ResultSet, error) {
	m.queryFilter = filter
	result := client.ResultSet{}
	for _, n := range fields {
		switch n {
		case "id", "count(*)":
			result = append(result, entity.NewColumnInt64(n, []int64{7}))
		case "name":
			result = append(result, entity.NewColumnVarChar(n, []string{"one"}))
		case "enabled":
			result = append(result, entity.NewColumnBool(n, []bool{true}))
		case "score":
			result = append(result, entity.NewColumnDouble(n, []float64{1.5}))
		case "meta":
			result = append(result, entity.NewColumnJSONBytes(n, [][]byte{[]byte(`{"x":1}`)}))
		case "vec":
			result = append(result, entity.NewColumnFloatVector(n, 3, [][]float32{{1, 2, 3}}))
		}
	}
	return result, m.fail
}
func TestMutations(t *testing.T) {
	m := &operationMock{schema: testSchema()}
	c := NewSession(context.Background(), m)
	for _, q := range []string{`create table items (id bigint primary key,v vector(3))`, `drop table items`} {
		if _, e := c.Execute(context.Background(), q); e != nil {
			t.Fatal(e)
		}
	}
	if !reflect.DeepEqual(m.calls, []string{"create", "drop"}) {
		t.Fatal(m.calls)
	}
	m.schema = testSchema()
	for _, verb := range []string{"INSERT", "UPSERT", "REPLACE"} {
		r, e := c.Execute(context.Background(), verb+` INTO items VALUES (7,'one',true,1.5,'{}',json_vector('[1,2,3]'))`)
		if e != nil {
			t.Fatal(e)
		}
		if r.AffectedRows != 1 {
			t.Fatal(r)
		}
	}
	if _, e := c.Execute(context.Background(), "delete from items where id in (1,2)"); e != nil {
		t.Fatal(e)
	}
	if m.queryFilter != "id in [1,2]" {
		t.Fatal(m.queryFilter)
	}
	for _, q := range []string{"delete from items", "delete from items limit 1", "delete from db.items where id=1", "insert ignore into items values (1)", "insert into db.items values (1)"} {
		if _, e := c.Execute(context.Background(), q); e == nil {
			t.Fatal(q)
		}
	}
	m.fail = errors.New("upstream failed")
	if _, e := c.Execute(context.Background(), "drop table items"); e == nil || !strings.Contains(e.Error(), "upstream") {
		t.Fatal(e)
	}
}
