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
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flike/kingshard/mysql"

	"sync"

	"github.com/flike/kingshard/backend"
	"github.com/flike/kingshard/config"
	"github.com/flike/kingshard/core/errors"
	"github.com/flike/kingshard/core/golog"
	"github.com/flike/kingshard/proxy/router"
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
	cfg   *config.Config
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
	schemas map[string]*Schema //user : schema of user

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

func NewServer(cfg *config.Config) (*Server, error) {
	s := new(Server)

	s.cfg = cfg
	s.counter = new(Counter)
	s.addr = cfg.Addr
	s.users = make(map[string]string)
	for _, user := range cfg.UserList {
		s.users[user.User] = user.Password
	}
	atomic.StoreInt32(&s.statusIndex, 0)
	s.status[s.statusIndex] = Online
	atomic.StoreInt32(&s.logSqlIndex, 0)
	s.logSql[s.logSqlIndex] = cfg.LogSql
	atomic.StoreInt32(&s.slowLogTimeIndex, 0)
	s.slowLogTime[s.slowLogTimeIndex] = cfg.SlowLogTime
	s.configVer = 0

	if len(cfg.Charset) == 0 {
		cfg.Charset = mysql.DEFAULT_CHARSET //utf8
	}
	cid, ok := mysql.CharsetIds[cfg.Charset]
	if !ok {
		return nil, errors.ErrInvalidCharset
	}
	//change the default charset
	mysql.DEFAULT_CHARSET = cfg.Charset
	mysql.DEFAULT_COLLATION_ID = cid
	mysql.DEFAULT_COLLATION_NAME = mysql.Collations[cid]

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

func (s *Server) newClientConn(co net.Conn) *ClientConn {
	c := new(ClientConn)
	tcpConn := co.(*net.TCPConn)

	//SetNoDelay controls whether the operating system should delay packet transmission
	// in hopes of sending fewer packets (Nagle's algorithm).
	// The default is true (no delay),
	// meaning that data is sent as soon as possible after a Write.
	//I set this option false.
	tcpConn.SetNoDelay(false)
	c.c = tcpConn

	func() {
		s.configUpdateMutex.RLock()
		defer s.configUpdateMutex.RUnlock()
		c.nodes = s.nodes
		// c.proxy = s
		c.configVer = s.configVer
	}()

	c.pkg = mysql.NewPacketIO(tcpConn)
	// c.proxy = s

	c.pkg.Sequence = 0

	c.connectionId = atomic.AddUint32(&baseConnId, 1)

	c.status = mysql.SERVER_STATUS_AUTOCOMMIT

	c.salt, _ = mysql.RandomBuf(20)

	c.txConns = make(map[*backend.Node]*backend.BackendConn)

	c.closed = false

	c.charset = mysql.DEFAULT_CHARSET
	c.collation = mysql.DEFAULT_COLLATION_ID

	c.stmtId = 0
	c.stmts = make(map[uint32]*Stmt)

	return c
}

func (s *Server) onConn(c net.Conn) {
	s.counter.IncrClientConns()
	conn := s.newClientConn(c) //新建一个conn

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

	conn.schema = s.GetSchema(conn.user)

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

func (s *Server) ChangeLogSql(v string) error {
	v = strings.ToLower(v)
	if v != golog.LogSqlOn && v != golog.LogSqlOff {
		return errors.ErrCmdUnsupport
	}
	if s.logSqlIndex == 0 {
		s.logSql[1] = v
		atomic.StoreInt32(&s.logSqlIndex, 1)
	} else {
		s.logSql[0] = v
		atomic.StoreInt32(&s.logSqlIndex, 0)
	}
	s.cfg.LogSql = v

	return nil
}

func (s *Server) ChangeSlowLogTime(v string) error {
	tmp, err := strconv.Atoi(v)
	if err != nil {
		return err
	}

	if s.slowLogTimeIndex == 0 {
		s.slowLogTime[1] = tmp
		atomic.StoreInt32(&s.slowLogTimeIndex, 1)
	} else {
		s.slowLogTime[0] = tmp
		atomic.StoreInt32(&s.slowLogTimeIndex, 0)
	}
	s.cfg.SlowLogTime = tmp

	return err
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

func (s *Server) DeleteSlave(node string, addr string) error {
	addr = strings.Split(addr, backend.WeightSplit)[0]
	n := s.GetNode(node)
	if n == nil {
		return fmt.Errorf("invalid node %s", node)
	}

	if err := n.DeleteSlave(addr); err != nil {
		return err
	}

	//sync node slave to global config
	for i, v1 := range s.cfg.Nodes {
		if node == v1.Name {
			s1 := strings.Split(v1.Slave, backend.SlaveSplit)
			s2 := make([]string, 0, len(s1)-1)
			for _, v2 := range s1 {
				hostPort := strings.Split(v2, backend.WeightSplit)[0]
				if addr != hostPort {
					s2 = append(s2, v2)
				}
			}
			s.cfg.Nodes[i].Slave = strings.Join(s2, backend.SlaveSplit)
		}
	}

	return nil
}

func (s *Server) AddSlave(node string, addr string) error {
	n := s.GetNode(node)
	if n == nil {
		return fmt.Errorf("invalid node %s", node)
	}

	if err := n.AddSlave(addr); err != nil {
		return err
	}

	//sync node slave to global config
	for i, v1 := range s.cfg.Nodes {
		if v1.Name == node {
			s1 := strings.Split(v1.Slave, backend.SlaveSplit)
			s1 = append(s1, addr)
			s.cfg.Nodes[i].Slave = strings.Join(s1, backend.SlaveSplit)
		}
	}

	return nil
}

func (s *Server) UpMaster(node string, addr string) error {
	n := s.GetNode(node)
	if n == nil {
		return fmt.Errorf("invalid node %s", node)
	}

	return n.UpMaster(addr)
}

func (s *Server) UpSlave(node string, addr string) error {
	n := s.GetNode(node)
	if n == nil {
		return fmt.Errorf("invalid node %s", node)
	}

	return n.UpSlave(addr)
}

func (s *Server) DownMaster(node, masterAddr string) error {
	n := s.GetNode(node)
	if n == nil {
		return fmt.Errorf("invalid node %s", node)
	}
	return n.DownMaster(masterAddr, backend.ManualDown)
}

func (s *Server) DownSlave(node, slaveAddr string) error {
	n := s.GetNode(node)
	if n == nil {
		return fmt.Errorf("invalid node [%s].", node)
	}
	return n.DownSlave(slaveAddr, backend.ManualDown)
}

func (s *Server) GetNode(name string) *backend.Node {
	return s.nodes[name]
}

func (s *Server) GetAllNodes() map[string]*backend.Node {
	return s.nodes
}

func (s *Server) GetSchema(user string) *Schema {
	return s.schemas[user]
}

func (s *Server) GetSlowLogTime() int {
	return s.slowLogTime[s.slowLogTimeIndex]
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
