package pkg

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestBinaryE2E crosses the actual CLI/config/process boundary. Its binary must
// be built separately, and the upstream must be a disposable Milvus instance.
func TestBinaryE2E(t *testing.T) {
	binary := os.Getenv("E2E_PROXY_BINARY")
	if binary == "" {
		t.Skip("set E2E_PROXY_BINARY to enable executable E2E")
	}
	addr := os.Getenv("MILVUS_TEST_ADDR")
	if addr == "" {
		t.Fatal("MILVUS_TEST_ADDR is required for binary E2E")
	}
	mode := os.Getenv("E2E_MODE")
	if mode != "mysql" && mode != "postgres" && mode != "both" {
		t.Fatal("E2E_MODE must be mysql, postgres, or both")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := os.Getenv("E2E_ARTIFACT_DIR")
	if artifacts == "" {
		artifacts = t.TempDir()
	}
	if err = os.MkdirAll(artifacts, 0700); err != nil {
		t.Fatal(err)
	}
	mysqlAddr, postgresAddr := reserveE2EPorts(t)
	config := filepath.Join(artifacts, "proxy-"+mode+".yaml")
	content := fmt.Sprintf("mode: %s\naddr: %q\npostgresAddr: %q\nuser: root\npassword: integration\nqueryTimeoutSeconds: 90\nmilvus:\n  addr: %q\n", mode, mysqlAddr, postgresAddr, addr)
	if err = os.WriteFile(config, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(artifacts, "proxy-"+mode+".log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		log.Close()
		if t.Failed() {
			if data, err := os.ReadFile(logPath); err == nil {
				t.Logf("proxy log:\n%s", data)
			}
		}
	})
	cmd := exec.Command(binary, "-config", config)
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var exitErr error
	go func() { exitErr = cmd.Wait(); close(done) }()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			select {
			case <-done:
				t.Errorf("proxy exited before SIGTERM: %v", exitErr)
				return
			default:
			}
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Error(err)
			}
			select {
			case <-done:
				if exitErr != nil {
					t.Errorf("proxy SIGTERM exit: %v", exitErr)
				}
			case <-time.After(10 * time.Second):
				cmd.Process.Kill()
				<-done
				t.Error("proxy did not exit within 10s of SIGTERM")
			}
		})
	}
	t.Cleanup(stop)
	modes := []string{mode}
	if mode == "both" {
		modes = []string{"mysql", "postgres"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, protocol := range modes {
		endpoint := mysqlAddr
		if protocol == "postgres" {
			endpoint = postgresAddr
		}
		for {
			err = probeE2E(ctx, protocol, endpoint)
			if err == nil {
				break
			}
			select {
			case <-done:
				t.Fatalf("proxy exited during readiness: %v", exitErr)
			case <-ctx.Done():
				t.Fatalf("%s readiness: %v", protocol, err)
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if mode == "mysql" {
		assertPortClosed(t, postgresAddr)
	}
	if mode == "postgres" {
		assertPortClosed(t, mysqlAddr)
	}
	testMilvusLifecycle(t, mysqlAddr, postgresAddr, modes)
	// Successful process exit and released listener ports are part of the contract.
	stop()
	assertPortClosed(t, mysqlAddr)
	assertPortClosed(t, postgresAddr)
}

func reserveE2EPorts(t *testing.T) (string, string) {
	t.Helper()
	a, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	return a.Addr().String(), b.Addr().String()
}
func assertPortClosed(t *testing.T, addr string) {
	t.Helper()
	co, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		co.Close()
		t.Errorf("unexpected listener at %s", addr)
	}
}
func probeE2E(ctx context.Context, protocol, addr string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if protocol == "mysql" {
		db, err := sql.Open("mysql", fmt.Sprintf("root:integration@tcp(%s)/default?timeout=2s", addr))
		if err != nil {
			return err
		}
		defer db.Close()
		var n int
		return db.QueryRowContext(probeCtx, "SELECT 1").Scan(&n)
	}
	pg, err := pgx.Connect(probeCtx, fmt.Sprintf("postgres://root:integration@%s/default?sslmode=disable", addr))
	if err != nil {
		return err
	}
	defer pg.Close(context.Background())
	var n int
	return pg.QueryRow(probeCtx, "SELECT 1").Scan(&n)
}
