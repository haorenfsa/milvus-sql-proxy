package pkg

import (
	"encoding/json"
	"reflect"
	"strconv"

	"github.com/cockroachdb/errors"
	"github.com/flike/kingshard/core/golog"
	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/xwb1989/sqlparser"
)

func (c *ClientConn) GetCollectinSchema(collectionName string) (*entity.Schema, error) {
	// TODO: cache
	collection, err := c.upstream.DescribeCollection(c.ctx, collectionName)
	if err != nil {
		return nil, errors.Wrapf(err, "describe collection[%s] failed", collectionName)
	}
	return collection.Schema, nil
}

func (c *ClientConn) handleInsert(stmt *sqlparser.Insert, args []interface{}) error {
	golog.Debug("conn", "handleInsert", "GetCollectinSchema", c.connectionId)
	schema, err := c.GetCollectinSchema(stmt.Table.Name.String())
	if err != nil {
		return c.writeError(err)
	}
	golog.Debug("conn", "handleInsert", "FillInsertColumns", c.connectionId)
	columns, err := c.FillInsertColumns(schema, stmt)
	if err != nil {
		return c.writeError(err)
	}

	golog.Debug("conn", "handleInsert", "upstream.Insert", c.connectionId)
	_, err = c.upstream.Insert(c.ctx,
		stmt.Table.Name.String(),
		"", // TODO: partition name
		columns...)
	if err != nil {
		return c.writeError(err)
	}
	golog.Debug("conn", "handleInsert", "writeOK", c.connectionId, "affectedRows", c.affectedRows)
	return c.writeOK(&mysql.Result{
		AffectedRows: uint64(len(stmt.Rows.(sqlparser.Values))),
	})
}

func (c *ClientConn) FillInsertColumns(schema *entity.Schema, stmt *sqlparser.Insert) ([]entity.Column, error) {
	rowsValues := stmt.Rows.(sqlparser.Values)
	if len(rowsValues) == 0 {
		return []entity.Column{}, nil
	}

	var columnSchmaMap = make(map[string]*entity.Field)
	for _, field := range schema.Fields {
		columnSchmaMap[field.Name] = field
	}

	var columnIndexMap = make(map[string]int)
	var columns []entity.Column = make([]entity.Column, 0, len(stmt.Columns))
	if len(stmt.Columns) > 0 {
		for columnIdx, columnStmt := range stmt.Columns {
			columnName := columnStmt.String()
			columnSchema, ok := columnSchmaMap[columnName]
			if !ok {
				return nil, errors.Errorf("column[%s] not exist", columnName)

			}
			golog.Debug("conn", "handleInsert", "insert", c.connectionId, "columnStmt", columnStmt)
			columnIndexMap[columnName] = columnIdx

			switch columnSchema.DataType {
			case entity.FieldTypeInt32:
				var values = make([]int32, len(rowsValues))
				for rowIdx, row := range rowsValues {
					columnValues := row[columnIdx]
					golog.Debug("conn", "handleInsert", "columnValues.(type)", c.connectionId, "expr", columnValues)
					switch expr := columnValues.(type) {
					case *sqlparser.SQLVal:
						if expr.Type != sqlparser.IntVal {
							return nil, errors.Errorf("column[%s] row[%d] type[%s] not [%s]", columnName, rowIdx, expr.Type, "IntVal")
						}
						val, err := strconv.ParseInt(string(expr.Val), 10, 32)
						if err != nil {
							return nil, errors.Wrapf(err, "column[%s] row[%d] type[%s] value[%v]", columnName, rowIdx, expr.Type, expr.Val)
						}
						values[rowIdx] = int32(val)
					case *sqlparser.FuncExpr:
						return nil, errors.Errorf("column[%s] row[%d] type[%s] not supported", columnName, rowIdx, expr.Name)
					}
				}
				columns = append(columns, entity.NewColumnInt32(columnName, values))
			case entity.FieldTypeInt64:
				var values = make([]int64, len(rowsValues))
				for rowIdx, row := range rowsValues {
					columnValues := row[columnIdx]
					golog.Debug("conn", "handleInsert", "columnValues.(type)", c.connectionId, "expr", columnValues)
					switch expr := columnValues.(type) {
					case *sqlparser.SQLVal:
						if expr.Type != sqlparser.IntVal {
							return nil, errors.Errorf("column[%s] row[%d] type[%s] not [%s]", columnName, rowIdx, expr.Type, "IntVal")
						}
						golog.Debug("conn", "handleInsert", "ParseInt", c.connectionId, "val", expr.Val)
						val, err := strconv.ParseInt(string(expr.Val), 10, 64)
						if err != nil {
							return nil, errors.Wrapf(err, "column[%s] row[%d] type[%s] value[%v]", columnName, rowIdx, expr.Type, expr.Val)
						}
						values[rowIdx] = val
					case *sqlparser.FuncExpr:
						return nil, errors.Errorf("column[%s] row[%d] type[%s] not supported", columnName, rowIdx, expr.Name)
					}
				}
				columns = append(columns, entity.NewColumnInt64(columnName, values))
			case entity.FieldTypeVarChar:
				var values = make([]string, len(rowsValues))
				for rowIdx, row := range rowsValues {
					columnValues := row[columnIdx]
					golog.Debug("conn", "handleInsert", "columnValues.(type)", c.connectionId, "expr", columnValues)
					switch expr := columnValues.(type) {
					case *sqlparser.SQLVal:
						if expr.Type != sqlparser.StrVal {
							return nil, errors.Errorf("column[%s] row[%d] type[%s] not [%s]", columnName, rowIdx, expr.Type, "StrVal")
						}
						values[rowIdx] = string(expr.Val)
					case *sqlparser.FuncExpr:
						return nil, errors.Errorf("column[%s] row[%d] type[%s] not supported", columnName, rowIdx, expr.Name)
					}
				}
				columns = append(columns, entity.NewColumnVarChar(columnName, values))
			case entity.FieldTypeFloatVector:
				var values = make([][]float32, len(rowsValues))
				for rowIdx, row := range rowsValues {
					columnValues := row[columnIdx]
					// golog.Debug("conn", "handleInsert", "columnValues.(type)", c.connectionId, "type", reflect.TypeOf(columnValues))
					switch expr := columnValues.(type) {
					case *sqlparser.SQLVal:
						return nil, errors.Errorf("column[%s] row[%d] type[%s] not supported", columnName, rowIdx, expr.Type)
					case *sqlparser.FuncExpr:
						const JSONVectorFuncName = "json_vector"
						if expr.Name.String() != JSONVectorFuncName {
							return nil, errors.Errorf("column[%s] row[%d] type[%s] not supported", columnName, rowIdx, expr.Name)
						}
						if len(expr.Exprs) != 1 {
							return nil, errors.Errorf("column[%s] row[%d] type[%s] len(exprs) != 1", columnName, rowIdx, expr.Name)
						}
						vectorExpr, ok := expr.Exprs[0].(*sqlparser.AliasedExpr)
						if !ok {
							return nil, errors.Errorf("column[%s] row[%d] type[%s] exprs[0] type != *sqlparser.AliasedExpr", columnName, rowIdx, expr.Name)
						}
						jsonVector := vectorExpr.Expr.(*sqlparser.SQLVal)
						golog.Debug("conn", "handleInsert", "jsonVector", c.connectionId, "jsonVector", string(jsonVector.Val))
						err := json.Unmarshal(jsonVector.Val, &values[rowIdx])
						if err != nil {
							return nil, errors.Wrap(err, "json.Unmarshal failed")
						}
					default:
						return nil, errors.Errorf("column[%s] row[%d] type[%s] not supported", columnName, rowIdx, reflect.TypeOf(expr).String())
					}
				}
				dim := len(values[0])
				golog.Debug("conn", "handleInsert", "insert", c.connectionId, "dim", dim)
				columns = append(columns, entity.NewColumnFloatVector(columnName, dim, values))
			// TODO: support more types
			default:
				return nil, errors.Errorf("column[%s] type[%s] not supported", columnName, columnSchema.DataType)
			}
		}
	}
	return columns, nil
}
