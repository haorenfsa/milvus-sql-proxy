package pkg

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/xwb1989/sqlparser"
)

func (c *ClientConn) parameterOIDs(ctx context.Context, q string, oids []uint32) ([]uint32, error) {
	if len(oids) == 0 {
		return oids, nil
	}
	args := make([]interface{}, len(oids))
	for i := range args {
		args[i] = fmt.Sprintf("__milvus_param_%d__", i+1)
	}
	normalized, _, err := bindSQL(q, "postgres", args, false)
	if err != nil {
		return nil, err
	}
	normalized = upsertRE.ReplaceAllString(normalized, "REPLACE INTO")
	stmt, err := sqlparser.Parse(normalized)
	if err != nil {
		for i := range oids {
			if oids[i] == 0 {
				oids[i] = 25
			}
		}
		return oids, nil
	}
	var table string
	switch s := stmt.(type) {
	case *sqlparser.Select:
		if len(s.From) == 1 {
			if a, ok := s.From[0].(*sqlparser.AliasedTableExpr); ok {
				if t, ok := a.Expr.(sqlparser.TableName); ok {
					table = t.Name.String()
				}
			}
		}
	case *sqlparser.Insert:
		table = s.Table.Name.String()
	case *sqlparser.Delete:
		if len(s.TableExprs) == 1 {
			if a, ok := s.TableExprs[0].(*sqlparser.AliasedTableExpr); ok {
				if t, ok := a.Expr.(sqlparser.TableName); ok {
					table = t.Name.String()
				}
			}
		}
	}
	fields := map[string]*entity.Field{}
	var schema *entity.Schema
	if table != "" && table != "dual" {
		old := c.ctx
		c.ctx = ctx
		schema, err = c.GetCollectinSchema(table)
		c.ctx = old
		if err != nil {
			return nil, err
		}
		for _, f := range schema.Fields {
			fields[f.Name] = f
		}
	}
	set := func(e sqlparser.Expr, oid uint32) {
		v, ok := e.(*sqlparser.SQLVal)
		if !ok {
			return
		}
		text := string(v.Val)
		if !strings.HasPrefix(text, "__milvus_param_") || !strings.HasSuffix(text, "__") {
			return
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(text, "__milvus_param_"), "__"))
		if err == nil && n > 0 && n <= len(oids) && oids[n-1] == 0 {
			oids[n-1] = oid
		}
	}
	fieldOID := func(f *entity.Field) uint32 {
		if f == nil {
			return 25
		}
		return pgOID(resultField(f.Name, f.DataType))
	}
	err = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		switch x := node.(type) {
		case *sqlparser.ComparisonExpr:
			if x == nil {
				return true, nil
			}
			if col, ok := x.Left.(*sqlparser.ColName); ok {
				oid := fieldOID(fields[col.Name.String()])
				set(x.Right, oid)
				if tuple, ok := x.Right.(sqlparser.ValTuple); ok {
					for _, e := range tuple {
						set(e, oid)
					}
				}
			}
			if col, ok := x.Right.(*sqlparser.ColName); ok {
				set(x.Left, fieldOID(fields[col.Name.String()]))
			}
		case *sqlparser.Limit:
			if x == nil {
				return true, nil
			}
			set(x.Rowcount, 20)
			if x.Offset != nil {
				set(x.Offset, 20)
			}
		}
		return true, nil
	}, stmt)
	if err != nil {
		return nil, err
	}
	if ins, ok := stmt.(*sqlparser.Insert); ok && schema != nil {
		names := []string{}
		for _, n := range ins.Columns {
			names = append(names, n.String())
		}
		if len(names) == 0 {
			for _, f := range schema.Fields {
				if !f.AutoID {
					names = append(names, f.Name)
				}
			}
		}
		if rows, ok := ins.Rows.(sqlparser.Values); ok {
			for _, row := range rows {
				for j, e := range row {
					if j < len(names) {
						set(e, fieldOID(fields[names[j]]))
					}
				}
			}
		}
	}
	for i := range oids {
		if oids[i] == 0 {
			oids[i] = 25
		}
	}
	return oids, nil
}
