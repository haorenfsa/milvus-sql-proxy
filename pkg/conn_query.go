package pkg

import (
	"fmt"
	"github.com/xwb1989/sqlparser"
	"regexp"
	"strings"
)

func (c *ClientConn) handleQuery(sql string) error {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return fmt.Errorf("empty SQL statement")
	}
	if handled, err := c.handleMilvusCommand(sql); handled {
		return err
	}
	if err := validateStatementKind(sql); err != nil {
		return err
	}
	stmt, err := sqlparser.ParseStrictDDL(normalizeTypes(sql))
	if err != nil {
		return err
	}
	if c.describe {
		switch stmt.(type) {
		case *sqlparser.Select, *sqlparser.Show:
		default:
			return c.writeOK(nil)
		}
	}
	switch v := stmt.(type) {
	case *sqlparser.Show:
		return c.handleShow(v, nil)
	case *sqlparser.DBDDL:
		return c.handleDBDDL(v, nil)
	case *sqlparser.DDL:
		return c.handleDDL(v, nil)
	case *sqlparser.Select:
		return c.handleSelect(v, nil)
	case *sqlparser.Insert:
		return c.handleInsert(v, nil)
	case *sqlparser.Delete:
		return c.handleDelete(v)
	case *sqlparser.Use:
		return c.handleUseDB(v.DBName.String(), nil)
	default:
		return fmt.Errorf("statement %T is not supported", stmt)
	}
}

var createTableSQL = regexp.MustCompile("(?is)^CREATE\\s+TABLE\\s+(?:[A-Za-z_][A-Za-z0-9_]*|`[A-Za-z_][A-Za-z0-9_]*`)\\s*\\(.*\\)$")
var nameOnlySQL = regexp.MustCompile("(?i)^(?:DROP TABLE|CREATE DATABASE|DROP DATABASE|USE)\\s+(?:[A-Za-z_][A-Za-z0-9_]*|`[A-Za-z_][A-Za-z0-9_]*`)$")

func validateStatementKind(q string) error {
	q = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(q), ";"))
	words := strings.Fields(q)
	if len(words) == 0 {
		return fmt.Errorf("empty statement")
	}
	switch strings.ToUpper(words[0]) {
	case "CREATE":
		if createTableSQL.MatchString(q) || nameOnlySQL.MatchString(q) {
			return nil
		}
	case "DROP", "USE":
		if nameOnlySQL.MatchString(q) {
			return nil
		}
	case "SHOW":
		if strings.EqualFold(q, "show tables") || strings.EqualFold(q, "show databases") {
			return nil
		}
	case "SELECT", "INSERT", "REPLACE", "DELETE":
		return nil
	}
	return fmt.Errorf("unsupported SQL statement or modifiers")
}
