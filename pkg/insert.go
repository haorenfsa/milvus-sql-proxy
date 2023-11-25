package pkg

import (
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/xwb1989/sqlparser"
)

func (c *ClientConn) handleInsert(stmt *sqlparser.Insert, args []interface{}) error {
	panic("todo")

	rowsValues := stmt.Rows.(sqlparser.Values)
	if len(rowsValues) == 0 {
		return c.writeOK(nil)
	}
	// TODO: cache

	// if c.upstream
	// row > 0, get co
	// entity.NewColumnInt32()
	// stmt.Columns.FindColumn()
	// for _, row := range rowsValues {
	// 	for _, value := range row {
	// 		switch expr := value.(type) {
	// 		case *sqlparser.SQLVal:
	// 			golog.Debug("conn", "handleInsert", "insert", c.connectionId, "expr", expr.Type)
	// 		case *sqlparser.FuncExpr:
	// 			golog.Debug("conn", "handleInsert", "insert", c.connectionId, "expr", expr.)
	// 		}
	// 	}
	// }
	// oneRow := rowsValues[0]

	// expr1 := oneRow[0].(*sqlparser.SQLVal)
	// expr2 := oneRow[1].(*sqlparser.FuncExpr)
	// var _ sqlparser.Expr
	// golog.Debug("conn", "handleInsert", "insert", c.connectionId, "expr1", expr1.Type, "expr2", expr2.Name)
	var columns []entity.Column
	c.upstream.Insert(c.ctx,
		stmt.Table.Name.String(),
		"", // TODO: partition name
		columns...)
	return c.writeOK(nil)
}
