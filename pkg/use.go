package pkg

import (
	"github.com/flike/kingshard/mysql"
)

func (c *ClientConn) handleUseDB(dbName string, args []interface{}) error {
	err := c.upstream.UsingDatabase(c.ctx, dbName)
	if err != nil {
		err = mysql.NewError(mysql.ER_DATABASE_NAME, err.Error())
		return c.writeError(err)
	}
	return c.writeOK(nil)
}
