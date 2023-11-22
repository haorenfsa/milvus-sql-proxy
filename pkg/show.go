package pkg

import (
	"strings"

	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/pkg/errors"
	"github.com/xwb1989/sqlparser"
)

const (
	DATABASES = "DATABASES"
)

func (c *ClientConn) handleShow(stmt *sqlparser.Show, args []interface{}) error {
	switch strings.ToUpper(stmt.Type) {
	case DATABASES:
		ret, err := c.upstream.ListDatabases(c.ctx)
		if err != nil {
			return mysql.NewError(mysql.ER_ABORTING_CONNECTION, errors.Wrap(err, "list databases failed").Error())
		}
		values := databasesToValues(ret)
		r, err := c.buildResultset(nil, []string{"DATABASES"}, values)
		if err != nil {
			return mysql.NewError(mysql.ER_UNKNOWN_ERROR, errors.Wrap(err, "build resultset failed").Error())
		}
		return c.writeResultset(c.status, r)
	default:
		return mysql.NewDefaultError(mysql.ER_UNKNOWN_ERROR, stmt.Type)
	}

}

func databasesToValues(dbs []entity.Database) [][]interface{} {
	column := make([]interface{}, 0, len(dbs))
	for _, db := range dbs {
		column = append(column, db.Name)
	}
	return [][]interface{}{column}
}

func formatCollections(cols []*entity.Collection) *mysql.Resultset {
	var field = &mysql.Field{}
	field.Name = []byte(DATABASES)
	for _, col := range cols {
		field.Data = append(field.Data, []byte(col.Name)...)
	}
	return &mysql.Resultset{
		Fields: []*mysql.Field{
			field,
		},
	}
}
