package pkg

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/xwb1989/sqlparser"
)

func (c *ClientConn) GetCollectinSchema(name string) (*entity.Schema, error) {
	col, err := c.upstream.DescribeCollection(c.ctx, name)
	if err != nil {
		return nil, err
	}
	if col == nil || col.Schema == nil {
		return nil, fmt.Errorf("collection has no schema")
	}
	return col.Schema, nil
}
func (c *ClientConn) handleInsert(s *sqlparser.Insert, _ []interface{}) error {
	if !s.Table.Qualifier.IsEmpty() || s.Ignore != "" || len(s.OnDup) > 0 || len(s.Partitions) > 0 {
		return fmt.Errorf("qualified tables, IGNORE, ON DUPLICATE KEY and partition clauses are not supported")
	}
	schema, err := c.GetCollectinSchema(s.Table.Name.String())
	if err != nil {
		return err
	}
	columns, err := c.FillInsertColumns(schema, s)
	if err != nil {
		return err
	}
	var ids entity.Column
	if s.Action == sqlparser.ReplaceStr {
		ids, err = c.upstream.Upsert(c.ctx, s.Table.Name.String(), "", columns...)
	} else {
		ids, err = c.upstream.Insert(c.ctx, s.Table.Name.String(), "", columns...)
	}
	if err != nil {
		return err
	}
	r := &mysql.Result{AffectedRows: uint64(len(s.Rows.(sqlparser.Values)))}
	if ids != nil && ids.Len() > 0 {
		if v, e := ids.Get(0); e == nil {
			if id, ok := v.(int64); ok && id >= 0 {
				r.InsertId = uint64(id)
			}
		}
	}
	return c.writeOK(r)
}
func (c *ClientConn) FillInsertColumns(schema *entity.Schema, s *sqlparser.Insert) ([]entity.Column, error) {
	rows, ok := s.Rows.(sqlparser.Values)
	if !ok || len(rows) == 0 {
		return nil, fmt.Errorf("INSERT requires VALUES rows")
	}
	fields := map[string]*entity.Field{}
	for _, f := range schema.Fields {
		fields[f.Name] = f
	}
	names := []string{}
	if len(s.Columns) == 0 {
		for _, f := range schema.Fields {
			if !f.AutoID {
				names = append(names, f.Name)
			}
		}
	} else {
		for _, n := range s.Columns {
			names = append(names, n.String())
		}
	}
	seen := map[string]bool{}
	for _, n := range names {
		f := fields[n]
		if f == nil {
			return nil, fmt.Errorf("unknown field %s", n)
		}
		if seen[n] {
			return nil, fmt.Errorf("duplicate field %s", n)
		}
		seen[n] = true
		if f.AutoID {
			return nil, fmt.Errorf("auto-ID field %s must be omitted", n)
		}
	}
	if s.Action == sqlparser.ReplaceStr {
		for _, f := range schema.Fields {
			if f.AutoID {
				return nil, fmt.Errorf("upsert requires an explicit primary key; auto-ID collections are unsupported")
			}
		}
	}
	for _, f := range schema.Fields {
		if !f.AutoID && !seen[f.Name] {
			return nil, fmt.Errorf("missing field %s", f.Name)
		}
	}
	for i, row := range rows {
		if len(row) != len(names) {
			return nil, fmt.Errorf("row %d has %d values, expected %d", i+1, len(row), len(names))
		}
	}
	columns := make([]entity.Column, 0, len(names))
	for j, name := range names {
		f := fields[name]
		column, err := emptyColumn(f)
		if err != nil {
			return nil, err
		}
		for i, row := range rows {
			v, err := fieldValue(f, row[j])
			if err != nil {
				return nil, fmt.Errorf("field %s row %d: %w", name, i+1, err)
			}
			if err = column.AppendValue(v); err != nil {
				return nil, err
			}
		}
		columns = append(columns, column)
	}
	return columns, nil
}
func emptyColumn(f *entity.Field) (entity.Column, error) {
	switch f.DataType {
	case entity.FieldTypeBool:
		return entity.NewColumnBool(f.Name, nil), nil
	case entity.FieldTypeInt8:
		return entity.NewColumnInt8(f.Name, nil), nil
	case entity.FieldTypeInt16:
		return entity.NewColumnInt16(f.Name, nil), nil
	case entity.FieldTypeInt32:
		return entity.NewColumnInt32(f.Name, nil), nil
	case entity.FieldTypeInt64:
		return entity.NewColumnInt64(f.Name, nil), nil
	case entity.FieldTypeFloat:
		return entity.NewColumnFloat(f.Name, nil), nil
	case entity.FieldTypeDouble:
		return entity.NewColumnDouble(f.Name, nil), nil
	case entity.FieldTypeVarChar:
		return entity.NewColumnVarChar(f.Name, nil), nil
	case entity.FieldTypeJSON:
		return entity.NewColumnJSONBytes(f.Name, nil), nil
	case entity.FieldTypeFloatVector:
		dim, e := strconv.Atoi(f.TypeParams["dim"])
		if e != nil || dim <= 0 {
			return nil, fmt.Errorf("invalid vector dimension")
		}
		return entity.NewColumnFloatVector(f.Name, dim, nil), nil
	default:
		return nil, fmt.Errorf("unsupported field type %s", f.DataType)
	}
}
func literalText(e sqlparser.Expr) (string, error) {
	switch v := e.(type) {
	case *sqlparser.SQLVal:
		if v.Type == sqlparser.StrVal || v.Type == sqlparser.IntVal || v.Type == sqlparser.FloatVal {
			return string(v.Val), nil
		}
	case sqlparser.BoolVal:
		return strconv.FormatBool(bool(v)), nil
	case *sqlparser.UnaryExpr:
		if v.Operator == "-" || v.Operator == "+" {
			if n, ok := v.Expr.(*sqlparser.SQLVal); ok && (n.Type == sqlparser.IntVal || n.Type == sqlparser.FloatVal) {
				return v.Operator + string(n.Val), nil
			}
		}
	}
	return "", fmt.Errorf("expected a literal value")
}
func fieldValue(f *entity.Field, e sqlparser.Expr) (interface{}, error) {
	if f.DataType == entity.FieldTypeFloatVector {
		if fn, ok := e.(*sqlparser.FuncExpr); ok {
			if !strings.EqualFold(fn.Name.String(), "json_vector") || len(fn.Exprs) != 1 {
				return nil, fmt.Errorf("expected json_vector(string)")
			}
			a, ok := fn.Exprs[0].(*sqlparser.AliasedExpr)
			if !ok {
				return nil, fmt.Errorf("invalid vector argument")
			}
			e = a.Expr
		}
	}
	text, err := literalText(e)
	if err != nil {
		return nil, err
	}
	switch f.DataType {
	case entity.FieldTypeBool:
		if text == "1" {
			return true, nil
		}
		if text == "0" {
			return false, nil
		}
		return strconv.ParseBool(text)
	case entity.FieldTypeInt8, entity.FieldTypeInt16, entity.FieldTypeInt32, entity.FieldTypeInt64:
		bits := map[entity.FieldType]int{entity.FieldTypeInt8: 8, entity.FieldTypeInt16: 16, entity.FieldTypeInt32: 32, entity.FieldTypeInt64: 64}[f.DataType]
		n, e := strconv.ParseInt(text, 10, bits)
		if e != nil {
			return nil, e
		}
		switch bits {
		case 8:
			return int8(n), nil
		case 16:
			return int16(n), nil
		case 32:
			return int32(n), nil
		}
		return n, nil
	case entity.FieldTypeFloat, entity.FieldTypeDouble:
		bits := 64
		if f.DataType == entity.FieldTypeFloat {
			bits = 32
		}
		n, e := strconv.ParseFloat(text, bits)
		if e != nil {
			return nil, e
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("non-finite float")
		}
		if bits == 32 {
			return float32(n), nil
		}
		return n, nil
	case entity.FieldTypeVarChar:
		v, ok := e.(*sqlparser.SQLVal)
		if !ok || v.Type != sqlparser.StrVal {
			return nil, fmt.Errorf("expected string literal")
		}
		max, err := strconv.Atoi(f.TypeParams["max_length"])
		if err != nil || len(text) > max {
			return nil, fmt.Errorf("string exceeds max_length")
		}
		return text, nil
	case entity.FieldTypeJSON:
		if !json.Valid([]byte(text)) {
			return nil, fmt.Errorf("invalid JSON")
		}
		return []byte(text), nil
	case entity.FieldTypeFloatVector:
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(text), &raw); err != nil {
			return nil, err
		}
		for _, x := range raw {
			if string(x) == "null" {
				return nil, fmt.Errorf("vector elements must be numbers")
			}
		}
		var v []float32
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			return nil, err
		}
		dim, _ := strconv.Atoi(f.TypeParams["dim"])
		if len(v) != dim {
			return nil, fmt.Errorf("vector dimension %d, expected %d", len(v), dim)
		}
		return v, nil
	}
	return nil, fmt.Errorf("unsupported field type %s", f.DataType)
}
func (c *ClientConn) handleDelete(s *sqlparser.Delete) error {
	if len(s.Targets) > 0 || len(s.Partitions) > 0 || s.Limit != nil || len(s.OrderBy) > 0 || s.Where == nil {
		return fmt.Errorf("DELETE requires WHERE; multi-table, partition, ORDER BY and LIMIT are unsupported")
	}
	if len(s.TableExprs) != 1 {
		return fmt.Errorf("DELETE requires one collection")
	}
	a, ok := s.TableExprs[0].(*sqlparser.AliasedTableExpr)
	if !ok || !a.As.IsEmpty() || a.Hints != nil {
		return fmt.Errorf("unsupported DELETE target")
	}
	t, ok := a.Expr.(sqlparser.TableName)
	if !ok || !t.Qualifier.IsEmpty() {
		return fmt.Errorf("unsupported DELETE target")
	}
	filter, err := scalarFilter(s.Where.Expr)
	if err != nil {
		return err
	}
	if err = c.upstream.Delete(c.ctx, t.Name.String(), "", filter); err != nil {
		return err
	}
	// SDK v2 Delete does not expose deleted row count. Do not fabricate one.
	return c.writeOK(nil)
}
