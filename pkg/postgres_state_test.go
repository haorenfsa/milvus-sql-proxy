package pkg

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
)

func rawPG(t *testing.T, s *Server) *pgproto3.Frontend {
	t.Helper()
	co, e := net.Dial("tcp", s.listeners[1].Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { co.Close() })
	co.SetDeadline(time.Now().Add(10 * time.Second))
	f := pgproto3.NewFrontend(co, co)
	f.Send(&pgproto3.StartupMessage{ProtocolVersion: 196608, Parameters: map[string]string{"user": "root", "database": "default"}})
	if e = f.Flush(); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Receive(); e != nil {
		t.Fatal(e)
	}
	f.Send(&pgproto3.PasswordMessage{Password: "secret"})
	f.Flush()
	for {
		msg, e := f.Receive()
		if e != nil {
			t.Fatal(e)
		}
		if _, ok := msg.(*pgproto3.ReadyForQuery); ok {
			break
		}
	}
	return f
}
func TestPostgresProtocolErrors(t *testing.T) {
	s := mockServer(t)
	for _, tc := range []struct {
		name     string
		messages []pgproto3.FrontendMessage
		bad      bool
	}{
		{"missing statement", []pgproto3.FrontendMessage{&pgproto3.Bind{PreparedStatement: "absent"}}, true},
		{"missing describe statement", []pgproto3.FrontendMessage{&pgproto3.Describe{ObjectType: 'S', Name: "absent"}}, true},
		{"missing describe portal", []pgproto3.FrontendMessage{&pgproto3.Describe{ObjectType: 'P', Name: "absent"}}, true},
		{"bad describe kind", []pgproto3.FrontendMessage{&pgproto3.Describe{ObjectType: 'Z'}}, true},
		{"missing execute portal", []pgproto3.FrontendMessage{&pgproto3.Execute{Portal: "absent"}}, true},
		{"invalid close kind", []pgproto3.FrontendMessage{&pgproto3.Close{ObjectType: 'Z'}}, true},
		{"malformed parse", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "select 'oops"}}, true},
		{"too many type hints", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "select 1", ParameterOIDs: []uint32{20}}}, true},
		{"duplicate statement", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "select 1"}, &pgproto3.Parse{Name: "a", Query: "select 1"}}, true},
		{"parameter count mismatch", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "select id from items where id=$1"}, &pgproto3.Bind{PreparedStatement: "a"}}, true},
		{"bad parameter format", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "select id from items where id=$1"}, &pgproto3.Bind{PreparedStatement: "a", Parameters: [][]byte{[]byte("1")}, ParameterFormatCodes: []int16{2}}}, true},
		{"bad result format", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "select id from items"}, &pgproto3.Bind{PreparedStatement: "a", ResultFormatCodes: []int16{2}}}, true},
		{"duplicate portal", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "select id from items"}, &pgproto3.Bind{PreparedStatement: "a", DestinationPortal: "p"}, &pgproto3.Bind{PreparedStatement: "a", DestinationPortal: "p"}}, true},
		{"mutation describe no data", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "create database demo"}, &pgproto3.Describe{ObjectType: 'S', Name: "a"}}, false},
		{"bind describe execute", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "select id from items"}, &pgproto3.Bind{PreparedStatement: "a", DestinationPortal: "p", ResultFormatCodes: []int16{1}}, &pgproto3.Describe{ObjectType: 'P', Name: "p"}, &pgproto3.Execute{Portal: "p", MaxRows: 1}, &pgproto3.Execute{Portal: "p"}, &pgproto3.Close{ObjectType: 'P', Name: "p"}, &pgproto3.Close{ObjectType: 'S', Name: "a"}, &pgproto3.Flush{}}, false},
		{"execution failure", []pgproto3.FrontendMessage{&pgproto3.Parse{Name: "a", Query: "delete from items"}, &pgproto3.Bind{PreparedStatement: "a"}, &pgproto3.Execute{}}, true},
		{"unsupported frontend message", []pgproto3.FrontendMessage{&pgproto3.CopyData{Data: []byte("bad")}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := rawPG(t, s)
			for _, m := range tc.messages {
				f.Send(m)
			}
			f.Send(&pgproto3.Sync{})
			if e := f.Flush(); e != nil {
				t.Fatal(e)
			}
			bad := false
			for {
				m, e := f.Receive()
				if e != nil {
					t.Fatal(e)
				}
				if _, ok := m.(*pgproto3.ErrorResponse); ok {
					bad = true
				}
				if _, ok := m.(*pgproto3.ReadyForQuery); ok {
					break
				}
			}
			if bad != tc.bad {
				t.Fatalf("error=%v wanted %v", bad, tc.bad)
			}
		})
	}
	for _, q := range []string{"", "select 'unterminated", "select nope from items"} {
		f := rawPG(t, s)
		f.Send(&pgproto3.Query{String: q})
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
	}
}
func TestFrontendTLS(t *testing.T) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "sqlproxy test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	priv, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if e = os.WriteFile(certPath, certPEM, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv}), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := NewServer(&Config{Mode: "both", Addr: "127.0.0.1:0", PostgresAddr: "127.0.0.1:0", User: "root", Password: "secret", TLSCert: certPath, TLSKey: keyPath})
	if e != nil {
		t.Fatal(e)
	}
	s.newSession = func(ctx context.Context) (*ClientConn, error) {
		return NewSession(ctx, &operationMock{schema: testSchema()}), nil
	}
	done := make(chan error, 1)
	go func() { done <- s.Run() }()
	defer func() { s.Close(); <-done }()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	if e = driver.RegisterTLSConfig("sqlproxy-test", &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}); e != nil {
		t.Fatal(e)
	}
	defer driver.DeregisterTLSConfig("sqlproxy-test")
	db, e := sql.Open("mysql", fmt.Sprintf("root:secret@tcp(%s)/default?tls=sqlproxy-test", s.listeners[0].Addr()))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = db.Ping(); e != nil {
		t.Fatal(e)
	}
	pg, e := pgx.Connect(context.Background(), fmt.Sprintf("postgres://root:secret@%s/default?sslmode=verify-full&sslrootcert=%s", s.listeners[1].Addr(), certPath))
	if e != nil {
		t.Fatal(e)
	}
	defer pg.Close(context.Background())
	var n int
	if e = pg.QueryRow(context.Background(), "SELECT 1").Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	// SSL negotiation falls back only when the client explicitly permits it.
	plain := mockServer(t)
	pg2, e := pgx.Connect(context.Background(), fmt.Sprintf("postgres://root:secret@%s/default?sslmode=prefer", plain.listeners[1].Addr()))
	if e != nil {
		t.Fatal(e)
	}
	pg2.Close(context.Background())
	if _, e := NewServer(&Config{TLSCert: "missing", TLSKey: "missing"}); e == nil {
		t.Fatal("invalid TLS files accepted")
	}
}
