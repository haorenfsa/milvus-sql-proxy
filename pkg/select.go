package pkg

import (
	"github.com/flike/kingshard/core/golog"
	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/pkg/errors"
	"github.com/xwb1989/sqlparser"
)

func (c *ClientConn) handleSelect(stmt *sqlparser.Select, args []interface{}) error {
	golog.Debug("conn", "handleSelect", "select", c.connectionId, "stmt", stmt, "len(froms)", len(stmt.From))
	froms := stmt.From
	if len(froms) > 1 {
		// TODO: support join?
		err := errors.Errorf("select from more than one table not supported")
		return c.writeError(err)
	}

	tableName := sqlparser.String(froms[0])

	tableSchema, err := c.GetCollectinSchema(tableName)
	if err != nil {
		return c.writeError(err)
	}
	// TODO: use real schema
	var outputFields []string
	var outputFieldsOrder = make(map[string]int)
	if len(stmt.SelectExprs) == 1 && sqlparser.String(stmt.SelectExprs[0]) == "*" {
		outputFields = make([]string, len(tableSchema.Fields))
		for i, field := range tableSchema.Fields {
			outputFields[i] = field.Name
		}
	} else {
		outputFields = make([]string, len(stmt.SelectExprs))
		for i, expr := range stmt.SelectExprs {
			outputFields[i] = sqlparser.String(expr)
		}
	}

	for i, field := range outputFields {
		outputFieldsOrder[field] = i
	}

	golog.Info("conn", "handleSelect", "upstream.Query", c.connectionId)
	// TODO: other expr, limits
	resp, err := c.upstream.Query(c.ctx, tableName, []string{}, "", outputFields, client.WithLimit(100))
	if err != nil {
		return c.writeError(err)
	}
	golog.Debug("conn", "handleSelect", "upstream.Query finished", c.connectionId, "rows", resp[0].Len(), "columns", len(resp))
	if len(resp) == 0 {
		return c.writeOK(nil)
	}

	ret := make([][]interface{}, resp[0].Len())
	for i := range ret {
		ret[i] = make([]interface{}, len(outputFields))
		for _, column := range resp {
			columnIdx, found := outputFieldsOrder[column.Name()]
			if !found {
				// id will always be returned, ignore
				continue
			}
			ret[i][columnIdx], err = column.Get(i)
			if err != nil {
				return c.writeError(err)
			}
		}
	}
	golog.Debug("conn", "buildResultset", "buildResultset", c.connectionId, "ret", ret)
	r, err := c.buildResultset(nil, outputFields, ret)
	if err != nil {
		return mysql.NewError(mysql.ER_UNKNOWN_ERROR, errors.Wrap(err, "build resultset failed").Error())
	}
	golog.Debug("conn", "handleSelect", "writeResultset", c.connectionId, "r", r)
	return c.writeResultset(c.status, r)
}
