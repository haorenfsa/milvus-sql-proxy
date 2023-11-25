package pkg

import (
	"fmt"

	"github.com/flike/kingshard/mysql"
	"github.com/xwb1989/sqlparser"
)

func (c *ClientConn) handleDBDDL(stmt *sqlparser.DBDDL, args []interface{}) error {
	switch stmt.Action {
	case sqlparser.CreateStr:
		return c.handleCreateDB(stmt, args)
	case sqlparser.DropStr:
		return c.handleDropDB(stmt, args)
	default:
		return mysql.NewError(mysql.ER_UNKNOWN_ERROR, fmt.Sprintf("create %s not supported", stmt.Action))
	}
}

func (c *ClientConn) handleCreateDB(stmt *sqlparser.DBDDL, args []interface{}) error {
	err := c.upstream.CreateDatabase(c.ctx, stmt.DBName)
	if err != nil {
		return mysql.NewError(mysql.ER_ABORTING_CONNECTION, err.Error())
	}
	return c.writeOK(&mysql.Result{
		AffectedRows: 1,
	})
}

func (c *ClientConn) handleDropDB(stmt *sqlparser.DBDDL, args []interface{}) error {
	err := c.upstream.DropDatabase(c.ctx, stmt.DBName)
	if err != nil {
		return mysql.NewError(mysql.ER_ABORTING_CONNECTION, err.Error())
	}
	return c.writeOK(nil)
}
