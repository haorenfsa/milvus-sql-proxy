package pkg

import (
	"fmt"
	"strings"

	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/pkg/errors"
	"github.com/xwb1989/sqlparser"
)

const (
	DatabasesStr = "DATABASES"
	TableStr     = "TABLES"
)

func (c *ClientConn) handleShow(stmt *sqlparser.Show, args []interface{}) error {
	switch strings.ToUpper(stmt.Type) {
	case DatabasesStr:
		ret, err := c.upstream.ListDatabases(c.ctx)
		if err != nil {
			return mysql.NewError(mysql.ER_ABORTING_CONNECTION, errors.Wrap(err, "list databases failed").Error())
		}
		if len(ret) == 0 {
			return c.writeOK(nil)
		}
		values := databasesToValues(ret)
		r, err := c.buildResultset(nil, []string{DatabasesStr}, values)
		if err != nil {
			return mysql.NewError(mysql.ER_UNKNOWN_ERROR, errors.Wrap(err, "build resultset failed").Error())
		}
		return c.writeResultset(c.status, r)
	case TableStr:
		ret, err := c.upstream.ListCollections(c.ctx)
		if err != nil {
			return mysql.NewError(mysql.ER_ABORTING_CONNECTION, errors.Wrap(err, "list collections failed").Error())
		}
		values := collectionsToValues(ret)
		r, err := c.buildResultset(nil, []string{TableStr}, values)
		if err != nil {
			return mysql.NewError(mysql.ER_UNKNOWN_ERROR, errors.Wrap(err, "build resultset failed").Error())
		}
		return c.writeResultset(c.status, r)
	default:
		return mysql.NewError(mysql.ER_UNKNOWN_ERROR, fmt.Sprintf("show %s not supported", stmt.Type))
	}

}

func databasesToValues(dbs []entity.Database) [][]interface{} {
	rows := make([][]interface{}, 0, len(dbs))
	for _, db := range dbs {
		column := []interface{}{
			db.Name,
		}
		rows = append(rows, column)
	}
	return rows
}

func collectionsToValues(cols []*entity.Collection) [][]interface{} {
	rows := make([][]interface{}, 0, len(cols))
	for _, col := range cols {
		column := []interface{}{
			col.Name,
		}
		rows = append(rows, column)
	}
	return rows
}
