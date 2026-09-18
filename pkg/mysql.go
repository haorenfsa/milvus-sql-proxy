package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	legacy "github.com/flike/kingshard/mysql"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/server"
)

type mysqlHandler struct {
	ctx     context.Context
	session *ClientConn
	timeout time.Duration
}

func (s *Server) serveMySQL(ctx context.Context, co net.Conn) {
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	session, err := s.newSession(startup)
	cancel()
	if err != nil {
		return
	}
	defer session.Close()
	h := &mysqlHandler{session: session, ctx: ctx, timeout: time.Duration(s.cfg.QueryTimeoutSeconds) * time.Second}
	conf := server.NewServer("8.0.11-milvus-sql-proxy", mysql.DEFAULT_COLLATION_ID, mysql.AUTH_NATIVE_PASSWORD, nil, s.tlsConfig)
	conn, err := conf.NewConn(co, s.cfg.User, s.cfg.Password, h)
	if err != nil {
		return
	}
	defer func() {
		if conn.Conn != nil {
			conn.Close()
		}
	}()
	co.SetDeadline(time.Time{})
	for {
		if err := conn.HandleCommand(); err != nil {
			return
		}
	}
}
func (h *mysqlHandler) UseDB(db string) error {
	ctx, cancel := context.WithTimeout(h.ctx, h.timeout)
	defer cancel()
	h.session.ctx = ctx
	return h.session.handleUseDB(db, nil)
}
func (h *mysqlHandler) HandleQuery(q string) (*mysql.Result, error) { return h.run(q, false) }
func (h *mysqlHandler) run(q string, binary bool) (*mysql.Result, error) {
	ctx, cancel := context.WithTimeout(h.ctx, h.timeout)
	defer cancel()
	r, err := h.session.Execute(ctx, q)
	if err != nil {
		return nil, err
	}
	return mysqlResult(r, binary)
}
func mysqlResult(r *legacy.Result, binary bool) (*mysql.Result, error) {
	out := &mysql.Result{AffectedRows: r.AffectedRows, InsertId: r.InsertId, Status: mysql.SERVER_STATUS_AUTOCOMMIT}
	if r.Resultset == nil {
		return out, nil
	}
	names := make([]string, len(r.Fields))
	for i, f := range r.Fields {
		names[i] = string(f.Name)
	}
	values := make([][]interface{}, len(r.Values))
	for i, row := range r.Values {
		values[i] = make([]interface{}, len(row))
		for j, v := range row {
			switch x := v.(type) {
			case bool:
				if x {
					v = int64(1)
				} else {
					v = int64(0)
				}
			case []float32:
				b, _ := json.Marshal(x)
				v = string(b)
			}
			values[i][j] = v
		}
	}
	rs, err := mysql.BuildSimpleResultset(names, values, binary)
	if err != nil {
		return nil, err
	}
	// Preserve field metadata for empty result sets as well as nonempty rows.
	for i, f := range r.Fields {
		if len(values) == 0 {
			rs.Fields[i] = &mysql.Field{Name: append([]byte(nil), f.Name...), Type: f.Type, Charset: f.Charset}
			rs.FieldNames[string(f.Name)] = i
		}
	}
	out.Resultset = rs
	return out, nil
}
func (h *mysqlHandler) HandleFieldList(string, string) ([]*mysql.Field, error) {
	return nil, fmt.Errorf("COM_FIELD_LIST is not supported; use DESCRIBE table")
}
func (h *mysqlHandler) HandleStmtPrepare(q string) (int, int, interface{}, error) {
	normalized, n, err := bindSQL(q, "mysql", nil, true)
	if err != nil {
		return 0, 0, nil, err
	}
	ctx, cancel := context.WithTimeout(h.ctx, h.timeout)
	defer cancel()
	r, err := h.session.Describe(ctx, normalized)
	if err != nil {
		return 0, 0, nil, err
	}
	cols := 0
	if r.Resultset != nil {
		cols = len(r.Fields)
	}
	return n, cols, nil, nil
}
func (h *mysqlHandler) HandleStmtExecute(_ interface{}, q string, args []interface{}) (*mysql.Result, error) {
	q, _, err := bindSQL(q, "mysql", args, false)
	if err != nil {
		return nil, err
	}
	return h.run(q, true)
}
func (h *mysqlHandler) HandleStmtClose(interface{}) error { return nil }
func (h *mysqlHandler) HandleOtherCommand(byte, []byte) error {
	return fmt.Errorf("unsupported MySQL command")
}
