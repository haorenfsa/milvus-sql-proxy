package pkg

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
)

type Server struct {
	cfg        *Config
	listeners  []net.Listener
	modes      []string
	mu         sync.Mutex
	conns      map[net.Conn]context.CancelFunc
	closed     bool
	wg         sync.WaitGroup
	tlsConfig  *tls.Config
	newSession func(context.Context) (*ClientConn, error)
}

func NewServer(cfg *Config) (*Server, error) {
	if err := cfg.defaults(); err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, conns: make(map[net.Conn]context.CancelFunc)}
	if cfg.TLSCert != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return nil, err
		}
		s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}
	s.newSession = func(ctx context.Context) (*ClientConn, error) {
		upstream, err := client.NewClient(ctx, client.Config{Address: cfg.Milvus.Address, Username: cfg.Milvus.Username, Password: cfg.Milvus.Password, APIKey: cfg.Milvus.APIKey, EnableTLSAuth: cfg.Milvus.EnableTLSAuth})
		if err != nil {
			return nil, err
		}
		return NewSession(ctx, upstream), nil
	}
	bind := func(mode, addr string) error {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		s.listeners = append(s.listeners, l)
		s.modes = append(s.modes, mode)
		return nil
	}
	if cfg.Mode == "mysql" || cfg.Mode == "both" {
		if err := bind("mysql", cfg.Addr); err != nil {
			s.Close()
			return nil, err
		}
	}
	if cfg.Mode == "postgres" || cfg.Mode == "both" {
		if err := bind("postgres", cfg.PostgresAddr); err != nil {
			s.Close()
			return nil, err
		}
	}
	return s, nil
}
func (s *Server) Run() error {
	errs := make(chan error, len(s.listeners))
	for i, l := range s.listeners {
		mode := s.modes[i]
		go func() { errs <- s.serve(l, mode) }()
	}
	var first error
	for range s.listeners {
		if err := <-errs; err != nil && first == nil {
			first = err
			s.Close()
		}
	}
	s.wg.Wait()
	return first
}
func (s *Server) serve(l net.Listener, mode string) error {
	for {
		co, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			co.Close()
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.conns[co] = cancel
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			defer co.Close()
			defer func() { s.mu.Lock(); delete(s.conns, co); s.mu.Unlock() }()
			// Malformed clients may cause third-party protocol decoders to panic. Keep
			// a single connection from taking down the listener.
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("%s connection failed: %v\n%s", mode, r, debug.Stack())
				}
			}()
			defer cancel()
			co.SetDeadline(time.Now().Add(15 * time.Second))
			if mode == "postgres" {
				s.servePostgres(ctx, co)
			} else {
				s.serveMySQL(ctx, co)
			}
		}()
	}
}
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	for _, l := range s.listeners {
		l.Close()
	}
	for co, cancel := range s.conns {
		cancel()
		co.Close()
	}
}
