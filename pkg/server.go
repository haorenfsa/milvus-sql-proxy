// partially copied & changed from : https://github.com/flike/kingshard/blob/master/proxy/server/server.go

// Copyright 2016 The kingshard Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.
package pkg

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-sdk-go/v2/client"

	"sync"

	"github.com/flike/kingshard/backend"
	"github.com/flike/kingshard/core/errors"
	"github.com/flike/kingshard/core/golog"
	"github.com/flike/kingshard/proxy/router"
	pkgErr "github.com/pkg/errors"
)

type Schema struct {
	nodes map[string]*backend.Node
	rule  *router.Router
}

type BlacklistSqls struct {
	sqls    map[string]string
	sqlsLen int
}

const (
	Offline = iota
	Online
	Unknown
)

type Server struct {
	cfg   *Config
	addr  string
	users map[string]string //user : psw

	statusIndex      int32
	status           [2]int32
	logSqlIndex      int32
	logSql           [2]string
	slowLogTimeIndex int32
	slowLogTime      [2]int

	counter *Counter
	nodes   map[string]*backend.Node

	listener net.Listener
	running  bool

	configUpdateMutex sync.RWMutex
	configVer         uint32
}

func (s *Server) Status() string {
	var status string
	switch s.status[s.statusIndex] {
	case Online:
		status = "online"
	case Offline:
		status = "offline"
	case Unknown:
		status = "unknown"
	default:
		status = "unknown"
	}
	return status
}

func NewServer(cfg *Config) (*Server, error) {
	s := new(Server)

	s.cfg = cfg
	s.counter = new(Counter)
	s.addr = cfg.Addr

	golog.Info("server", "NewServer", "addr", 0, "addr", s.addr)
	atomic.StoreInt32(&s.statusIndex, 0)
	s.status[s.statusIndex] = Online
	s.configVer = 0

	var err error
	netProto := "tcp"

	s.listener, err = net.Listen(netProto, s.addr)

	if err != nil {
		return nil, err
	}

	golog.Info("server", "NewServer", "Server running", 0,
		"netProto",
		netProto,
		"address",
		s.addr)
	return s, nil
}

func (s *Server) flushCounter() {
	for {
		s.counter.FlushCounter()
		time.Sleep(1 * time.Second)
	}
}

func (s *Server) newClientConn(ctx context.Context, co net.Conn) (*ClientConn, error) {
	c := new(ClientConn)
	c.ctx = ctx
	tcpConn := co.(*net.TCPConn)

	//SetNoDelay controls whether the operating system should delay packet transmission
	// in hopes of sending fewer packets (Nagle's algorithm).
	// The default is true (no delay),
	// meaning that data is sent as soon as possible after a Write.
	//I set this option false.
	tcpConn.SetNoDelay(false)
	c.c = tcpConn

	c.pkg = mysql.NewPacketIO(tcpConn)
	// c.proxy = s

	var err error
	cfg := s.cfg
	milvusCfg := client.Config{
		Address:  cfg.Milvus.Address,
		Username: cfg.Milvus.Username,
		Password: cfg.Milvus.Password,
		APIKey:   cfg.Milvus.APIKey,
	}
	c.upstream, err = client.NewClient(ctx, milvusCfg)
	if err != nil {
		return nil, pkgErr.Wrap(err, "connect to milvus failed")
	}

	c.pkg.Sequence = 0

	c.connectionId = atomic.AddUint32(&baseConnId, 1)

	c.status = mysql.SERVER_STATUS_AUTOCOMMIT

	c.salt, _ = mysql.RandomBuf(20)

	c.closed = false

	c.charset = mysql.DEFAULT_CHARSET
	c.collation = mysql.DEFAULT_COLLATION_ID

	c.stmtId = 0
	c.stmts = make(map[uint32]*Stmt)

	return c, nil
}

func (s *Server) onConn(c net.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.counter.IncrClientConns()
	conn, err := s.newClientConn(ctx, c) //新建一个conn
	if err != nil {
		conn.writeError(err)
		conn.Close()
		return
	}

	defer func() {
		err := recover()
		if err != nil {
			const size = 4096
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)] //获得当前goroutine的stacktrace
			golog.Error("server", "onConn", "error", 0,
				"remoteAddr", c.RemoteAddr().String(),
				"stack", string(buf),
			)
		}

		conn.Close()
		s.counter.DecrClientConns()
	}()

	if err := conn.Handshake(); err != nil {
		golog.Error("server", "onConn", err.Error(), 0)
		conn.writeError(err)
		conn.Close()
		return
	}

	conn.Run()
}

func (s *Server) ChangeProxy(v string) error {
	var status int32
	switch v {
	case "online":
		status = Online
	case "offline":
		status = Offline
	default:
		status = Unknown
	}
	if status == Unknown {
		return errors.ErrCmdUnsupport
	}

	if s.statusIndex == 0 {
		s.status[1] = status
		atomic.StoreInt32(&s.statusIndex, 1)
	} else {
		s.status[0] = status
		atomic.StoreInt32(&s.statusIndex, 0)
	}

	return nil
}

func (s *Server) Run() error {
	s.running = true

	// flush counter
	go s.flushCounter()

	for s.running {
		conn, err := s.listener.Accept()
		if err != nil {
			golog.Error("server", "Run", err.Error(), 0)
			continue
		}

		go s.onConn(conn)
	}

	return nil
}

func (s *Server) Close() {
	s.running = false
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *Server) GetMonitorData() map[string]map[string]string {
	data := make(map[string]map[string]string)

	// get all node's monitor data
	for _, node := range s.nodes {
		//get master monitor data
		dbData := make(map[string]string)
		idleConns, cacheConns, pushConnCount, popConnCount := node.Master.ConnCount()

		dbData["idleConn"] = strconv.Itoa(idleConns)
		dbData["cacheConns"] = strconv.Itoa(cacheConns)
		dbData["pushConnCount"] = strconv.FormatInt(pushConnCount, 10)
		dbData["popConnCount"] = strconv.FormatInt(popConnCount, 10)
		dbData["maxConn"] = fmt.Sprintf("%d", node.Cfg.MaxConnNum)
		dbData["type"] = "master"

		data[node.Master.Addr()] = dbData

		//get all slave monitor data
		for _, slaveNode := range node.Slave {
			slaveDbData := make(map[string]string)
			idleConns, cacheConns, pushConnCount, popConnCount := slaveNode.ConnCount()

			slaveDbData["idleConn"] = strconv.Itoa(idleConns)
			slaveDbData["cacheConns"] = strconv.Itoa(cacheConns)
			slaveDbData["pushConnCount"] = strconv.FormatInt(pushConnCount, 10)
			slaveDbData["popConnCount"] = strconv.FormatInt(popConnCount, 10)
			slaveDbData["maxConn"] = fmt.Sprintf("%d", node.Cfg.MaxConnNum)
			slaveDbData["type"] = "slave"

			data[slaveNode.Addr()] = slaveDbData
		}
	}

	return data
}
