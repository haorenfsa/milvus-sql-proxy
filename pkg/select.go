package pkg

import (
	"fmt"
	"strings"

	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/xwb1989/sqlparser"
)

func (c *ClientConn) handleSelect(stmt *sqlparser.Select, _ []interface{}) error {
	if len(stmt.From) == 1 && sqlparser.String(stmt.From[0]) == "dual" && stmt.Where == nil && stmt.Limit == nil && stmt.Distinct == "" && len(stmt.GroupBy) == 0 && stmt.Having == nil && len(stmt.OrderBy) == 0 && stmt.Lock == "" && len(stmt.SelectExprs) == 1 {
		expr := strings.ToLower(sqlparser.String(stmt.SelectExprs[0]))
		switch expr {
		case "1":
			return c.rows([]string{"1"}, [][]interface{}{{int64(1)}})
		case "version()":
			return c.rows([]string{"version"}, [][]interface{}{{"milvus-sql-proxy"}})
		case "database()", "current_database()":
			return c.rows([]string{"database"}, [][]interface{}{{c.db}})
		}
	}
	plan, err := planSelect(stmt)
	if err != nil {
		return err
	}
	schema, err := c.GetCollectinSchema(plan.table)
	if err != nil {
		return err
	}
	schemaFields := map[string]*entity.Field{}
	var pk string
	for _, f := range schema.Fields {
		schemaFields[f.Name] = f
		if f.PrimaryKey {
			pk = f.Name
		}
	}
	if plan.vectorField != "" && schemaFields["_distance"] != nil {
		return fmt.Errorf("_distance is reserved for vector search scores")
	}
	names := []string{}
	types := []entity.FieldType{}
	if len(stmt.SelectExprs) == 1 && sqlparser.String(stmt.SelectExprs[0]) == "*" {
		for _, f := range schema.Fields {
			names = append(names, f.Name)
			types = append(types, f.DataType)
		}
	} else {
		for _, expr := range stmt.SelectExprs {
			name := sqlparser.String(expr)
			if strings.EqualFold(name, "count(*)") {
				name = "count(*)"
			}
			if name != "count(*)" {
				name = expr.(*sqlparser.AliasedExpr).Expr.(*sqlparser.ColName).Name.String()
			}
			switch {
			case name == "count(*)":
				if plan.vectorField != "" {
					return fmt.Errorf("count with vector search is unsupported")
				}
				names = append(names, name)
				types = append(types, entity.FieldTypeInt64)
			case name == "_distance" && plan.vectorField != "":
				names = append(names, name)
				types = append(types, entity.FieldTypeFloat)
			default:
				f := schemaFields[name]
				if f == nil {
					return fmt.Errorf("unknown field %s", name)
				}
				names = append(names, name)
				types = append(types, f.DataType)
			}
		}
	}
	fields := make([]*mysql.Field, len(names))
	for i, n := range names {
		fields[i] = resultField(n, types[i])
	}
	result := func(rows [][]interface{}) error {
		r, err := c.buildResultset(fields, names, rows)
		if err != nil {
			return err
		}
		r.Fields = fields
		return c.writeResultset(c.status, r)
	}
	if c.describe || plan.limit == 0 {
		return result(nil)
	}
	outputs := []string{}
	seen := map[string]bool{}
	for _, n := range names {
		if (n != "_distance" || plan.vectorField == "") && !seen[n] {
			outputs = append(outputs, n)
			seen[n] = true
		}
	}
	var cols client.ResultSet
	if plan.vectorField != "" {
		f := schemaFields[plan.vectorField]
		if f == nil || f.DataType != entity.FieldTypeFloatVector {
			return fmt.Errorf("search requires a FLOAT_VECTOR field")
		}
		v, e := fieldValue(f, plan.vectorExpr)
		if e != nil {
			return e
		}
		indexes, e := c.vectorIndexes(plan.table, plan.vectorField)
		if e != nil {
			return e
		}
		if len(indexes) == 0 {
			return fmt.Errorf("vector field has no index")
		}
		params := indexes[0].Params()
		metric := entity.MetricType(params["metric_type"])
		if metric == "" {
			return fmt.Errorf("vector index has no metric_type")
		}
		sp := &querySearchParams{values: map[string]interface{}{}}
		switch strings.ToUpper(string(indexes[0].IndexType())) {
		case "HNSW":
			ef := plan.limit + plan.offset
			if ef < 64 {
				ef = 64
			}
			sp.values["ef"] = ef
		case "IVF_FLAT", "IVF_SQ8", "IVF_PQ":
			sp.values["nprobe"] = 16
		}
		rs, e := c.upstream.Search(c.ctx, plan.table, nil, plan.filter, outputs, []entity.Vector{entity.FloatVector(v.([]float32))}, plan.vectorField, metric, int(plan.limit), sp, client.WithOffset(plan.offset), client.WithSearchQueryConsistencyLevel(entity.ClStrong))
		if e != nil {
			return e
		}
		if len(rs) == 0 {
			return result(nil)
		}
		r := rs[0]
		if r.Err != nil {
			return r.Err
		}
		cols = r.Fields
		if r.IDs != nil && cols.GetColumn(pk) == nil {
			cols = append(cols, r.IDs)
		}
		rows := make([][]interface{}, r.ResultCount)
		for i := range rows {
			rows[i] = make([]interface{}, len(names))
			for j, n := range names {
				if n == "_distance" {
					if i >= len(r.Scores) {
						return fmt.Errorf("incomplete search scores")
					}
					rows[i][j] = r.Scores[i]
					continue
				}
				col := cols.GetColumn(n)
				if col == nil && n == pk {
					col = r.IDs
				}
				if col == nil {
					return fmt.Errorf("missing result field %s", n)
				}
				rows[i][j], err = col.Get(i)
				if err != nil {
					return err
				}
			}
		}
		return result(rows)
	}
	opts := []client.SearchQueryOptionFunc{client.WithSearchQueryConsistencyLevel(entity.ClStrong)}
	if len(names) != 1 || names[0] != "count(*)" {
		opts = append(opts, client.WithLimit(plan.limit), client.WithOffset(plan.offset))
	} else if stmt.Limit != nil {
		return fmt.Errorf("LIMIT on count(*) is unsupported")
	}
	cols, err = c.upstream.Query(c.ctx, plan.table, nil, plan.filter, outputs, opts...)
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return result(nil)
	}
	rows := make([][]interface{}, cols[0].Len())
	for i := range rows {
		rows[i] = make([]interface{}, len(names))
		for j, n := range names {
			col := cols.GetColumn(n)
			if col == nil {
				return fmt.Errorf("missing result field %s", n)
			}
			rows[i][j], err = col.Get(i)
			if err != nil {
				return err
			}
		}
	}
	return result(rows)
}
func resultField(name string, t entity.FieldType) *mysql.Field {
	f := &mysql.Field{Name: []byte(name), Charset: 63}
	switch t {
	case entity.FieldTypeBool:
		f.Type = mysql.MYSQL_TYPE_TINY
	case entity.FieldTypeInt8, entity.FieldTypeInt16, entity.FieldTypeInt32, entity.FieldTypeInt64:
		f.Type = mysql.MYSQL_TYPE_LONGLONG
	case entity.FieldTypeFloat, entity.FieldTypeDouble:
		f.Type = mysql.MYSQL_TYPE_DOUBLE
	default:
		f.Type = mysql.MYSQL_TYPE_VAR_STRING
		f.Charset = 33
	}
	return f
}

type querySearchParams struct{ values map[string]interface{} }

func (p *querySearchParams) Params() map[string]interface{} { return p.values }
func (p *querySearchParams) AddRadius(v float64)            { p.values["radius"] = v }
func (p *querySearchParams) AddRangeFilter(v float64)       { p.values["range_filter"] = v }
