package pkg

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/xwb1989/sqlparser"

	"reflect"
	"testing"
)

func parsedSelect(t *testing.T, q string) *sqlparser.Select {
	t.Helper()
	s, e := sqlparser.Parse(q)
	if e != nil {
		t.Fatal(e)
	}
	return s.(*sqlparser.Select)
}
func TestSelectPlan(t *testing.T) {
	for _, tc := range []struct {
		sql, filter   string
		limit, offset int64
	}{
		{"select * from docs", "", 100, 0},
		{"select id from docs where id=1 limit 3 offset 2", "id == 1", 3, 2},
		{"select id from docs where name='a' and (id<>2 or id>=3) limit 2,4", `(name == "a" and ((id != 2 or id >= 3)))`, 4, 2},
		{"select id from docs where not (id < -1)", "not ((id < -1))", 100, 0},
		{"select id from docs where enabled=true", "enabled == true", 100, 0},
		{"select id from docs limit 0", "", 0, 0},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			p, e := planSelect(parsedSelect(t, tc.sql))
			if e != nil {
				t.Fatal(e)
			}
			if p.table != "docs" || p.filter != tc.filter || p.limit != tc.limit || p.offset != tc.offset {
				t.Fatalf("unexpected plan: %+v", p)
			}
		})
	}
}
func TestUnsupportedSelect(t *testing.T) {
	for _, q := range []string{
		"select id from docs order by id", "select distinct id from docs", "select id from docs group by id", "select id from docs having id=1", "select id from docs for update",
		"select * from docs, other", "select * from docs join other on docs.id=other.id", "select * from (select * from docs) d", "select * from docs d", "select * from db.docs", "select docs.* from docs", "select *, id from docs", "select id as x from docs", "select docs.id from docs", "select * from docs use index (idx)",
		"select id from docs where id is null", "select id from docs where id=abs(1)", "select id from docs where abs(id)=1", "select id from docs where id=1 and id is null", "select id from docs where id is null or id=1", "select id from docs where db.id=1", "select id from docs limit :n", "select id from docs limit 9999999999999999999999999", "select id from docs limit 1 offset :n",
	} {
		t.Run(q, func(t *testing.T) {
			if _, e := planSelect(parsedSelect(t, q)); e == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

type selectMock struct {
	queryErr, schemaErr error
	client.Client
	filter string
	fields []string
	opts   client.SearchQueryOption
	calls  int
	result client.ResultSet
}

func (m *selectMock) DescribeCollection(context.Context, string) (*entity.Collection, error) {
	if m.schemaErr != nil {
		return nil, m.schemaErr
	}
	return &entity.Collection{Schema: &entity.Schema{Fields: []*entity.Field{{Name: "id"}, {Name: "name"}}}}, nil
}
func (m *selectMock) Query(_ context.Context, _ string, _ []string, filter string, fields []string, opts ...client.SearchQueryOptionFunc) (client.ResultSet, error) {
	m.calls++
	m.filter = filter
	m.fields = fields
	for _, o := range opts {
		o(&m.opts)
	}
	return m.result, m.queryErr
}

func TestSelectSDKAndWire(t *testing.T) {
	for _, empty := range []bool{false, true} {
		m := &selectMock{}
		if !empty {
			m.result = client.ResultSet{entity.NewColumnInt64("id", []int64{7}), entity.NewColumnVarChar("name", []string{"alice"})}
		}

		c := &ClientConn{ctx: context.Background(), upstream: m}
		if e := c.handleSelect(parsedSelect(t, "select name,id from docs where id=7 limit 2 offset 3"), nil); e != nil {
			t.Fatal(e)
		}
		if m.filter != "id == 7" || m.opts.Limit != 2 || m.opts.Offset != 3 || !reflect.DeepEqual(m.fields, []string{"name", "id"}) {
			t.Fatalf("wrong SDK call: %+v", m)
		}
		if c.result == nil || len(c.result.Fields) != 2 {
			t.Fatal("expected 2-column result")
		}
		if !empty && c.result.Values[0][0] != "alice" {
			t.Fatal("missing result row")
		}
	}
}
func TestSelectZeroLimit(t *testing.T) {
	m := &selectMock{}

	c := &ClientConn{ctx: context.Background(), upstream: m}
	if e := c.handleSelect(parsedSelect(t, "select * from docs limit 0"), nil); e != nil {
		t.Fatal(e)
	}
	if m.calls != 0 {
		t.Fatal("LIMIT 0 queried upstream")
	}
}

func TestSelectErrors(t *testing.T) {
	for _, tc := range []struct {
		q string
		m *selectMock
	}{
		{"select id from docs order by id", &selectMock{}},
		{"select id from docs", &selectMock{schemaErr: fmt.Errorf("schema unavailable")}},
		{"select id from docs", &selectMock{queryErr: fmt.Errorf("query unavailable")}},
	} {

		c := &ClientConn{ctx: context.Background(), upstream: tc.m}
		if e := c.handleSelect(parsedSelect(t, tc.q), nil); e == nil {
			t.Fatal("expected error")
		}
	}
}
func TestSelectRepeatedProjection(t *testing.T) {
	m := &selectMock{result: client.ResultSet{entity.NewColumnInt64("id", []int64{7})}}

	c := &ClientConn{ctx: context.Background(), upstream: m}
	if e := c.handleSelect(parsedSelect(t, "select id,id from docs"), nil); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(c.result.Values[0], []interface{}{int64(7), int64(7)}) {
		t.Fatal("repeated projection lost a value")
	}
}
