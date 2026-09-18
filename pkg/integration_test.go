package pkg

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
)

func startTestServer(t *testing.T, cfg *Config) *Server {
	t.Helper()
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Run() }()
	t.Cleanup(func() {
		s.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server shutdown hung")
		}
	})
	return s
}

// Enabled explicitly to avoid connecting ordinary unit tests to any Milvus.
func TestMilvusIntegration(t *testing.T) {
	addr := os.Getenv("MILVUS_TEST_ADDR")
	if addr == "" {
		t.Skip("set MILVUS_TEST_ADDR for disposable Milvus integration")
	}
	s := startTestServer(t, &Config{Mode: "both", Addr: "127.0.0.1:0", PostgresAddr: "127.0.0.1:0", User: "root", Password: "integration", QueryTimeoutSeconds: 90, Milvus: MilvusConfig{Address: addr}})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, mode := range []string{"mysql", "postgres"} {
		t.Run(mode, func(t *testing.T) {
			dbname := fmt.Sprintf("sqlproxy_%s_%d", mode, time.Now().UnixNano())
			var exec func(string, ...interface{}) error
			var query func(string, ...interface{}) ([][]interface{}, error)
			if mode == "mysql" {
				db, e := sql.Open("mysql", fmt.Sprintf("root:integration@tcp(%s)/default", s.listeners[0].Addr()))
				if e != nil {
					t.Fatal(e)
				}
				defer db.Close()
				db.SetMaxOpenConns(1)
				exec = func(q string, args ...interface{}) error { _, e := db.ExecContext(ctx, q, args...); return e }
				query = func(q string, args ...interface{}) ([][]interface{}, error) {
					rs, e := db.QueryContext(ctx, q, args...)
					if e != nil {
						return nil, e
					}
					defer rs.Close()
					cols, e := rs.Columns()
					if e != nil {
						return nil, e
					}
					out := [][]interface{}{}
					for rs.Next() {
						values := make([]interface{}, len(cols))
						ptrs := make([]interface{}, len(cols))
						for i := range ptrs {
							ptrs[i] = &values[i]
						}
						if e := rs.Scan(ptrs...); e != nil {
							return nil, e
						}
						for i, v := range values {
							if b, ok := v.([]byte); ok {
								values[i] = string(b)
							}
						}
						out = append(out, values)
					}
					return out, rs.Err()
				}
			} else {
				pg, e := pgx.Connect(ctx, fmt.Sprintf("postgres://root:integration@%s/default?sslmode=disable", s.listeners[1].Addr()))
				if e != nil {
					t.Fatal(e)
				}
				defer pg.Close(ctx)
				exec = func(q string, args ...interface{}) error { _, e := pg.Exec(ctx, q, args...); return e }
				query = func(q string, args ...interface{}) ([][]interface{}, error) {
					rs, e := pg.Query(ctx, q, args...)
					if e != nil {
						return nil, e
					}
					defer rs.Close()
					out := [][]interface{}{}
					for rs.Next() {
						v, e := rs.Values()
						if e != nil {
							return nil, e
						}
						out = append(out, v)
					}
					return out, rs.Err()
				}
			}
			mustExec := func(q string, args ...interface{}) {
				t.Helper()
				if e := exec(q, args...); e != nil {
					t.Fatalf("%s: %v", q, e)
				}
			}
			mustQuery := func(q string, args ...interface{}) [][]interface{} {
				t.Helper()
				v, e := query(q, args...)
				if e != nil {
					t.Fatalf("%s: %v", q, e)
				}
				return v
			}
			mustExec("CREATE DATABASE " + dbname)
			defer func() {
				exec("USE " + dbname)
				exec("RELEASE TABLE items")
				exec("DROP TABLE items")
				exec("USE default")
				if e := exec("DROP DATABASE " + dbname); e != nil {
					t.Error(e)
				}
			}()
			mustExec("USE " + dbname)
			mustExec("CREATE TABLE items (id bigint PRIMARY KEY, name varchar(100), enabled bool, score double, meta json, embedding vector(3))")
			mustExec("CREATE INDEX embedding_idx ON items (embedding) USING HNSW WITH (metric_type='L2', M=16, efConstruction=100)")
			mustExec("INSERT INTO items VALUES (1,'one',true,1.5,'{\"tag\":1}',json_vector('[1,0,0]')), (2,'two',false,2.5,'{\"tag\":2}',json_vector('[0,1,0]'))")
			mustExec("LOAD TABLE items")
			if v := mustQuery("SHOW INDEXES FROM items"); len(v) != 1 {
				t.Fatalf("indexes: %v", v)
			}
			if v := mustQuery("DESCRIBE items"); len(v) != 6 {
				t.Fatalf("describe: %v", v)
			}
			if v := mustQuery("SELECT id,name,enabled,score,meta,embedding FROM items WHERE id=1"); len(v) != 1 {
				t.Fatalf("rows: %v", v)
			}
			if v := mustQuery("SELECT id FROM items WHERE id=999"); len(v) != 0 {
				t.Fatal(v)
			}
			if v := mustQuery("SELECT count(*) FROM items"); len(v) != 1 {
				t.Fatal(v)
			}
			if v := mustQuery("SELECT id,_distance FROM items WHERE embedding LIKE json_vector('[1,0,0]') LIMIT 1"); len(v) != 1 || fmt.Sprint(v[0][0]) != "1" {
				t.Fatalf("search: %v", v)
			}
			placeholder := "?"
			if mode == "postgres" {
				placeholder = "$1"
			}
			if v := mustQuery("SELECT id,name FROM items WHERE id="+placeholder, int64(2)); len(v) != 1 {
				t.Fatalf("parameter query: %v", v)
			}
			mustExec("UPSERT INTO items VALUES (2,'updated',true,3.5,'{}',json_vector('[0,0,1]'))")
			if v := mustQuery("SELECT name FROM items WHERE id=2"); len(v) != 1 {
				t.Fatal(v)
			}
			mustExec("DELETE FROM items WHERE id=2")
			if v := mustQuery("SELECT id FROM items WHERE id=2"); len(v) != 0 {
				t.Fatalf("delete: %v", v)
			}
			for _, bad := range []string{"DROP VIEW items", "SELECT 1 HAVING 1=0", "DELETE FROM items", "INSERT INTO items VALUES (3,'bad',true,1,'{}',json_vector('[null,0,1]'))"} {
				if e := exec(bad); e == nil {
					t.Fatalf("accepted unsupported SQL %s", bad)
				}
			}
			mustQuery("SELECT id FROM items WHERE id=1")
			mustExec("FLUSH TABLE items")
			mustExec("RELEASE TABLE items")
			mustExec("DROP INDEX embedding_idx ON items")
			for _, method := range []string{"FLAT", "IVF_FLAT", "AUTOINDEX"} {
				mustExec("CREATE INDEX embedding_idx ON items (embedding) USING " + method + " WITH (metric_type='L2')")
				mustExec("LOAD TABLE items")
				if v := mustQuery("SELECT id FROM items WHERE embedding LIKE json_vector('[1,0,0]') LIMIT 1"); len(v) != 1 {
					t.Fatal(method, v)
				}
				mustExec("RELEASE TABLE items")
				mustExec("DROP INDEX embedding_idx ON items")
			}
			mustExec("CREATE INDEX name_idx ON items (name) USING INVERTED")
			if v := mustQuery("SHOW INDEXES FROM items"); len(v) != 1 {
				t.Fatal(v)
			}
			mustExec("DROP INDEX name_idx ON items")

			mustExec("CREATE PARTITION extra ON items")
			if v := mustQuery("SHOW PARTITIONS FROM items"); len(v) != 2 {
				t.Fatal(v)
			}
			mustExec("DROP PARTITION extra ON items")
			mustExec("DROP TABLE items")
		})
	}
	// Incorrect credentials must fail both protocol handshakes.
	for i, mode := range []string{"mysql", "postgres"} {
		if mode == "mysql" {
			db, e := sql.Open("mysql", fmt.Sprintf("root:wrong@tcp(%s)/default", s.listeners[i].Addr()))
			if e != nil {
				t.Fatal(e)
			}
			if e = db.PingContext(ctx); e == nil {
				t.Fatal("MySQL accepted wrong password")
			}
			db.Close()
		} else {
			pg, e := pgx.Connect(ctx, fmt.Sprintf("postgres://root:wrong@%s/default?sslmode=disable", s.listeners[i].Addr()))
			if e == nil {
				pg.Close(ctx)
				t.Fatal("PG accepted wrong password")
			}
			if !strings.Contains(e.Error(), "authentication") {
				t.Fatal(e)
			}
		}
	}
}
