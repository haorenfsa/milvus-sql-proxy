package pkg

import (
	"context"
	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"sync"
)

// ClientConn is a SQL session. Each session owns its Milvus client because
// UsingDatabase changes client state. Transport adapters never share sessions.
type ClientConn struct {
	sync.Mutex
	ctx          context.Context
	upstream     client.Client
	db           string
	connectionId uint32
	status       uint16
	affectedRows int64
	result       *mysql.Result
	describe     bool
}

func NewSession(ctx context.Context, upstream client.Client) *ClientConn {
	return &ClientConn{ctx: ctx, upstream: upstream, db: "default", status: mysql.SERVER_STATUS_AUTOCOMMIT}
}

func (c *ClientConn) Execute(ctx context.Context, sql string) (*mysql.Result, error) {
	c.ctx = ctx
	c.result = nil
	if err := c.handleQuery(sql); err != nil {
		return nil, err
	}
	if c.result == nil {
		c.result = &mysql.Result{}
	}
	return c.result, nil
}

// Describe obtains SELECT metadata without executing queries or mutations.
func (c *ClientConn) Describe(ctx context.Context, sql string) (*mysql.Result, error) {
	c.describe = true
	defer func() { c.describe = false }()
	return c.Execute(ctx, sql)
}
func (c *ClientConn) writeOK(r *mysql.Result) error {
	if r == nil {
		r = &mysql.Result{}
	}
	c.result = r
	return nil
}
func (c *ClientConn) writeError(err error) error { return err }
func (c *ClientConn) Close() {
	if c.upstream != nil {
		c.upstream.Close()
	}
}
