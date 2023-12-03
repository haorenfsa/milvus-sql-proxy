// partially copied & changed from : https://github.com/flike/kingshard/blob/master/proxy/server/

// Copyright 2016 The kingshard Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package pkg

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/flike/kingshard/core/errors"
	"github.com/flike/kingshard/core/golog"
	"github.com/xwb1989/sqlparser"
)

/*处理query语句*/
func (c *ClientConn) handleQuery(sql string) (err error) {
	defer func() {
		if e := recover(); e != nil {
			golog.OutputSql("Error", "err:%v,sql:%s", e, sql)

			if err, ok := e.(error); ok {
				const size = 4096
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]

				golog.Error("ClientConn", "handleQuery",
					err.Error(), 0,
					"stack", string(buf), "sql", sql)
			}

			err = errors.ErrInternalServer
			return
		}
	}()

	sql = strings.TrimRight(sql, ";") //删除sql语句最后的分号
	golog.Debug("conn", "handleQuery", sql, c.connectionId)
	var stmt sqlparser.Statement
	stmt, err = sqlparser.Parse(sql) //解析sql语句,得到的stmt是一个interface
	if err != nil {
		golog.Error("conn", "parse", err.Error(), c.connectionId, "sql", sql)
		return err
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
	// TODO:
	// case *sqlparser.Update:
	// 	return c.handleExec(stmt, nil)
	// case *sqlparser.Delete:
	// 	return c.handleExec(stmt, nil)
	// case *sqlparser.Set:
	// 	return c.handleSet(v, sql)
	// case *sqlparser.Begin:
	// 	return c.handleBegin()
	// case *sqlparser.Commit:
	// 	return c.handleCommit()
	// case *sqlparser.Rollback:
	// 	return c.handleRollback()
	// case *sqlparser.Admin:
	// 	if c.user == "root" {
	// 		return c.handleAdmin(v)
	// 	}
	// 	return fmt.Errorf("statement %T not support now", stmt)
	// case *sqlparser.AdminHelp:
	// 	if c.user == "root" {
	// 		return c.handleAdminHelp(v)
	// 	}
	// 	return fmt.Errorf("statement %T not support now", stmt)
	// case *sqlparser.UseDB:
	// 	return c.handleUseDB(v.DB)
	// case *sqlparser.SimpleSelect:
	// 	return c.handleSimpleSelect(v)
	// case *sqlparser.Truncate:
	// 	return c.handleExec(stmt, nil)
	default:
		return fmt.Errorf("statement %T not support now", stmt)
	}
}

func (c *ClientConn) handleExec(stmt sqlparser.Statement, args []interface{}) error {
	panic("not implemented")
}
