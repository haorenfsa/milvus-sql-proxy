package pkg

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
)

func mockServer(t *testing.T) *Server {
	t.Helper()
	s, e := NewServer(&Config{Mode: "both", Addr: "127.0.0.1:0", PostgresAddr: "127.0.0.1:0", User: "root", Password: "secret"})
	if e != nil {
		t.Fatal(e)
	}
	s.newSession = func(ctx context.Context) (*ClientConn, error) {
		return NewSession(ctx, &operationMock{schema: testSchema(), db: "default"}), nil
	}
	done := make(chan error, 1)
	go func() { done <- s.Run() }()
	t.Cleanup(func() {
		s.Close()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(5 * time.Second):
			t.Error("shutdown hung")
		}
	})
	return s
}
func TestMySQLDriver(t *testing.T) {
	s := mockServer(t)
	db, e := sql.Open("mysql", fmt.Sprintf("root:secret@tcp(%s)/otherdb", s.listeners[0].Addr()))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var name string
	var id int64
	var flag bool
	var score float64
	if e = db.QueryRow("SELECT name,id,enabled,score FROM items WHERE id=?", int64(7)).Scan(&name, &id, &flag, &score); e != nil {
		t.Fatal(e)
	}
	if name != "one" || id != 7 || !flag || score != 1.5 {
		t.Fatal(name, id, flag, score)
	}
	if e = db.QueryRow("SELECT database()").Scan(&name); e != nil || name != "otherdb" {
		t.Fatal(name, e)
	}
	if _, e = db.Exec("BEGIN"); e == nil {
		t.Fatal("accepted transaction")
	}
	if e = db.Ping(); e != nil {
		t.Fatal(e)
	}
	bad, _ := sql.Open("mysql", fmt.Sprintf("root:bad@tcp(%s)/default", s.listeners[0].Addr()))
	defer bad.Close()
	if e = bad.Ping(); e == nil {
		t.Fatal("accepted bad password")
	}
}
func TestPostgresDriver(t *testing.T) {
	s := mockServer(t)
	ctx := context.Background()
	pg, e := pgx.Connect(ctx, fmt.Sprintf("postgres://root:secret@%s/otherdb?sslmode=disable", s.listeners[1].Addr()))
	if e != nil {
		t.Fatal(e)
	}
	defer pg.Close(ctx)
	var name string
	var id int64
	var flag bool
	var score float64
	if e = pg.QueryRow(ctx, "SELECT name,id,enabled,score FROM items WHERE id=$1", int64(7)).Scan(&name, &id, &flag, &score); e != nil {
		t.Fatal(e)
	}
	if name != "one" || id != 7 || !flag || score != 1.5 {
		t.Fatal(name, id, flag, score)
	}
	if e = pg.QueryRow(ctx, "SELECT current_database()").Scan(&name); e != nil || name != "otherdb" {
		t.Fatal(name, e)
	}
	rows, e := pg.Query(ctx, "DESCRIBE items")
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for rows.Next() {
		var field, kind, params string
		var primary, auto bool
		if e := rows.Scan(&field, &kind, &primary, &auto, &params); e != nil {
			t.Fatal(e)
		}
		count++
	}
	if e := rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	if count != 6 {
		t.Fatal(count)
	}
	if _, e = pg.Exec(ctx, "BEGIN"); e == nil {
		t.Fatal("accepted transaction")
	}
	if e = pg.QueryRow(ctx, "SELECT 1").Scan(&id); e != nil || id != 1 {
		t.Fatal(id, e)
	}
	// Simple protocol uses PostgreSQL identifier/literal quoting too.
	if e = pg.QueryRow(ctx, `SELECT "id" FROM items WHERE name='it''s \\ literal'`, pgx.QueryExecModeSimpleProtocol).Scan(&id); e != nil {
		t.Fatal(e)
	}
	bad, e := pgx.Connect(ctx, fmt.Sprintf("postgres://root:bad@%s/default?sslmode=disable", s.listeners[1].Addr()))
	if e == nil {
		bad.Close(ctx)
		t.Fatal("accepted bad password")
	}
}
func TestBindSQL(t *testing.T) {
	for _, tc := range []struct {
		q, d string
		args []interface{}
		want string
		n    int
	}{
		{"select ?,'?',`?`,\"?\" -- ?\n from x where id=?", "mysql", []interface{}{int64(1), "a' OR 1=1 --"}, "select 1,'?',`?`,\"?\" -- ?\n from x where id='a\\' OR 1=1 --'", 2},
		{`select "id" from t where id=$1 or id=$1 /* $2 */`, "postgres", []interface{}{int64(7)}, "select `id` from t where id=7 or id=7 /* $2 */", 1},
		{`select 'it''s' from t where x=$1`, "postgres", []interface{}{`a\b`}, `select 'it\'s' from t where x='a\\b'`, 1},
	} {
		got, n, e := bindSQL(tc.q, tc.d, tc.args, false)
		if e != nil || got != tc.want || n != tc.n {
			t.Fatalf("%q => %q %d %v", tc.q, got, n, e)
		}
	}
	for _, q := range []string{"select $0", "select $99999999999999999", "select $$str$$", "select 'oops", "select /*oops"} {
		if _, _, e := bindSQL(q, "postgres", nil, false); e == nil {
			t.Fatal(q)
		}
	}
	if _, _, e := bindSQL("select ?", "mysql", nil, false); e == nil {
		t.Fatal("missing arg accepted")
	}
	if _, _, e := bindSQL("select 1", "mysql", []interface{}{1}, false); e == nil {
		t.Fatal("extra arg accepted")
	}
}
func TestPGEncodings(t *testing.T) {
	for _, tc := range []struct {
		oid uint32
		v   interface{}
	}{{16, true}, {16, false}, {20, int64(-9)}, {701, float64(1.25)}, {25, "str"}} {
		b, e := pgValue(tc.v, tc.oid, 1)
		if e != nil {
			t.Fatal(e)
		}
		v, e := pgParameter(b, tc.oid, 1)
		if e != nil || !reflect.DeepEqual(v, tc.v) {
			t.Fatal(v, e)
		}
	}
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, 42)
	if v, e := pgParameter(b, 23, 1); e != nil || v != int32(42) {
		t.Fatal(v, e)
	}
	if _, e := pgParameter([]byte{1}, 20, 1); e == nil {
		t.Fatal("invalid int accepted")
	}
	if v, e := pgParameter(nil, 25, 0); e != nil || v != nil {
		t.Fatal(v, e)
	}
	if validFormats([]int16{2}, 1) || validFormats([]int16{0, 1}, 3) {
		t.Fatal("bad formats accepted")
	}
}
func TestShutdownCancelsDial(t *testing.T) {
	s, e := NewServer(&Config{Addr: "127.0.0.1:0"})
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	s.newSession = func(ctx context.Context) (*ClientConn, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }
	done := make(chan error, 1)
	go func() { done <- s.Run() }()
	co, e := net.Dial("tcp", s.listeners[0].Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer co.Close()
	<-entered
	s.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel upstream dial")
	}
}
func TestConfig(t *testing.T) {
	for _, data := range []string{"mode: unsupported", "queryTimeoutSeconds: -1", "tlsCert: file"} {
		if _, e := ParseConfigData([]byte(data)); e == nil {
			t.Fatal(data)
		}
	}
	cfg, e := ParseConfigData([]byte("mode: both"))
	if e != nil || cfg.User != "root" || !strings.HasPrefix(cfg.Addr, "127.0.0.1") {
		t.Fatal(cfg, e)
	}
}

// Low-level extended protocol regression: errors recover at Sync and a failed
// mutation is never executed during Parse/Describe/Bind.
func TestPostgresExtendedRecovery(t *testing.T) {
	s := mockServer(t)
	co, e := net.Dial("tcp", s.listeners[1].Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer co.Close()
	co.SetDeadline(time.Now().Add(5 * time.Second))
	f := pgproto3.NewFrontend(co, co)
	f.Send(&pgproto3.StartupMessage{ProtocolVersion: 196608, Parameters: map[string]string{"user": "root", "database": "default"}})
	f.Flush()
	if _, e = f.Receive(); e != nil {
		t.Fatal(e)
	}
	f.Send(&pgproto3.PasswordMessage{Password: "secret"})
	f.Flush()
	for {
		m, e := f.Receive()
		if e != nil {
			t.Fatal(e)
		}
		if _, ok := m.(*pgproto3.ReadyForQuery); ok {
			break
		}
	}
	f.Send(&pgproto3.Parse{Name: "bad", Query: "SELECT id FROM items WHERE id=$1"})
	f.Send(&pgproto3.Bind{PreparedStatement: "bad", Parameters: [][]byte{[]byte("invalid int")}})
	f.Send(&pgproto3.Execute{})
	f.Send(&pgproto3.Sync{})
	f.Flush()
	sawError := false
	for {
		m, e := f.Receive()
		if e != nil {
			t.Fatal(e)
		}
		if _, ok := m.(*pgproto3.ErrorResponse); ok {
			sawError = true
		}
		if _, ok := m.(*pgproto3.ReadyForQuery); ok {
			break
		}
	}
	if !sawError {
		t.Fatal("expected bind error")
	}
	f.Send(&pgproto3.Query{String: "SELECT 1"})
	f.Flush()
	for {
		m, e := f.Receive()
		if e != nil {
			t.Fatal(e)
		}
		if _, ok := m.(*pgproto3.ErrorResponse); ok {
			t.Fatal(m)
		}
		if _, ok := m.(*pgproto3.ReadyForQuery); ok {
			break
		}
	}
}
