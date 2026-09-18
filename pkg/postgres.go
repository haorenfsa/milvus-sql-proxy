package pkg

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	legacy "github.com/flike/kingshard/mysql"
	"github.com/jackc/pgx/v5/pgproto3"
)

type pgStatement struct {
	query string
	oids  []uint32
}
type pgPortal struct {
	query     string
	formats   []int16
	result    *legacy.Result
	pos       int
	completed bool
}

func (s *Server) servePostgres(ctx context.Context, co net.Conn) {
	b := pgproto3.NewBackend(co, co)
	b.SetMaxBodyLen(16 << 20)
	startup, err := b.ReceiveStartupMessage()
	if err != nil {
		return
	}
	if _, ok := startup.(*pgproto3.SSLRequest); ok {
		if s.tlsConfig == nil {
			if _, err = co.Write([]byte("N")); err != nil {
				return
			}
		} else {
			if _, err = co.Write([]byte("S")); err != nil {
				return
			}
			t := tls.Server(co, s.tlsConfig)
			if err = t.HandshakeContext(ctx); err != nil {
				return
			}
			co = t
			b = pgproto3.NewBackend(co, co)
			b.SetMaxBodyLen(16 << 20)
		}
		startup, err = b.ReceiveStartupMessage()
		if err != nil {
			return
		}
	}
	start, ok := startup.(*pgproto3.StartupMessage)
	if !ok {
		return
	}
	fail := func(e error) { b.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "0A000", Message: e.Error()}) }
	b.Send(&pgproto3.AuthenticationCleartextPassword{})
	if b.Flush() != nil {
		return
	}
	auth, e := b.Receive()
	if e != nil {
		return
	}
	pw, ok := auth.(*pgproto3.PasswordMessage)
	if !ok || subtle.ConstantTimeCompare([]byte(start.Parameters["user"]), []byte(s.cfg.User)) != 1 || subtle.ConstantTimeCompare([]byte(pw.Password), []byte(s.cfg.Password)) != 1 {
		b.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: "authentication failed"})
		b.Flush()
		return
	}
	startupCtx, startupCancel := context.WithTimeout(ctx, 15*time.Second)
	defer startupCancel()
	session, err := s.newSession(startupCtx)
	if err != nil {
		fail(err)
		b.Flush()
		return
	}
	defer session.Close()
	db := start.Parameters["database"]
	if db == "" {
		db = "default"
	}
	session.ctx = startupCtx
	if err = session.handleUseDB(db, nil); err != nil {
		fail(err)
		b.Flush()
		return
	}
	b.Send(&pgproto3.AuthenticationOk{})
	for _, p := range [][2]string{{"server_version", "14.0"}, {"client_encoding", "UTF8"}, {"standard_conforming_strings", "on"}, {"DateStyle", "ISO, MDY"}, {"TimeZone", "UTC"}} {
		b.Send(&pgproto3.ParameterStatus{Name: p[0], Value: p[1]})
	}
	b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if b.Flush() != nil {
		return
	}
	co.SetDeadline(time.Time{})
	statements := map[string]pgStatement{}
	portals := map[string]*pgPortal{}
	failed := false
	timeout := time.Duration(s.cfg.QueryTimeoutSeconds) * time.Second
	execute := func(q string, describe bool) (*legacy.Result, error) {
		qctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if describe {
			return session.Describe(qctx, q)
		}
		return session.Execute(qctx, q)
	}
	for {
		msg, err := b.Receive()
		if err != nil {
			return
		}
		if _, ok := msg.(*pgproto3.Terminate); ok {
			return
		}
		if failed {
			if _, ok := msg.(*pgproto3.Sync); !ok {
				continue
			}
		}
		var opErr error
		switch m := msg.(type) {
		case *pgproto3.Query:
			if strings.TrimSpace(m.String) == "" {
				b.Send(&pgproto3.EmptyQueryResponse{})
			} else {
				q, _, e := bindSQL(m.String, "postgres", nil, false)
				if e != nil {
					opErr = e
				} else {
					r, e := execute(q, false)
					if e != nil {
						opErr = e
					} else {
						if r.Resultset != nil {
							b.Send(pgDescription(r, nil))
							opErr = pgRows(b, r, 0, len(r.Values), nil)
						}
						if opErr == nil {
							b.Send(&pgproto3.CommandComplete{CommandTag: []byte(pgTag(q, r))})
						}
					}
				}
			}
			if opErr != nil {
				fail(opErr)
				opErr = nil
			}
			b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		case *pgproto3.Parse:
			if len(statements) >= 1024 && m.Name != "" {
				opErr = fmt.Errorf("too many prepared statements")
				break
			}
			_, n, e := bindSQL(m.Query, "postgres", nil, true)
			if e != nil {
				opErr = e
				break
			}
			if len(m.ParameterOIDs) > n {
				opErr = fmt.Errorf("too many parameter types")
				break
			}
			oids := make([]uint32, n)
			copy(oids, m.ParameterOIDs)
			parseCtx, parseCancel := context.WithTimeout(ctx, timeout)
			oids, e = session.parameterOIDs(parseCtx, m.Query, oids)
			parseCancel()
			if e != nil {
				opErr = e
				break
			}
			if _, exists := statements[m.Name]; exists && m.Name != "" {
				opErr = fmt.Errorf("prepared statement already exists")
				break
			}
			statements[m.Name] = pgStatement{m.Query, oids}
			b.Send(&pgproto3.ParseComplete{})
		case *pgproto3.Describe:
			var q string
			var formats []int16
			if m.ObjectType == 'S' {
				st, exists := statements[m.Name]
				if !exists {
					opErr = fmt.Errorf("unknown prepared statement")
					break
				}
				b.Send(&pgproto3.ParameterDescription{ParameterOIDs: st.oids})
				q, _, opErr = bindSQL(st.query, "postgres", nil, true)
			} else if m.ObjectType == 'P' {
				p, exists := portals[m.Name]
				if !exists {
					opErr = fmt.Errorf("unknown portal")
					break
				}
				q = p.query
				formats = p.formats
			} else {
				opErr = fmt.Errorf("invalid describe target")
				break
			}
			if opErr == nil {
				r, e := execute(q, true)
				if e != nil {
					opErr = e
				} else if r.Resultset == nil {
					b.Send(&pgproto3.NoData{})
				} else {
					b.Send(pgDescription(r, formats))
				}
			}
		case *pgproto3.Bind:
			st, exists := statements[m.PreparedStatement]
			if !exists {
				opErr = fmt.Errorf("unknown prepared statement")
				break
			}
			if len(m.Parameters) != len(st.oids) {
				opErr = fmt.Errorf("parameter count mismatch")
				break
			}
			if !validFormats(m.ParameterFormatCodes, len(m.Parameters)) {
				opErr = fmt.Errorf("invalid parameter formats")
				break
			}
			args := make([]interface{}, len(m.Parameters))
			for i, data := range m.Parameters {
				args[i], opErr = pgParameter(data, st.oids[i], formatAt(m.ParameterFormatCodes, i))
				if opErr != nil {
					break
				}
			}
			if opErr != nil {
				break
			}
			q, _, e := bindSQL(st.query, "postgres", args, false)
			if e != nil {
				opErr = e
				break
			}
			r, e := execute(q, true)
			if e != nil {
				opErr = e
				break
			}
			cols := 0
			if r.Resultset != nil {
				cols = len(r.Fields)
			}
			if !validFormats(m.ResultFormatCodes, cols) {
				opErr = fmt.Errorf("invalid result formats")
				break
			}
			if len(portals) >= 1024 && m.DestinationPortal != "" {
				opErr = fmt.Errorf("too many portals")
				break
			}
			if _, exists := portals[m.DestinationPortal]; exists && m.DestinationPortal != "" {
				opErr = fmt.Errorf("portal already exists")
				break
			}
			portals[m.DestinationPortal] = &pgPortal{query: q, formats: append([]int16(nil), m.ResultFormatCodes...)}
			b.Send(&pgproto3.BindComplete{})
		case *pgproto3.Execute:
			p, exists := portals[m.Portal]
			if !exists {
				opErr = fmt.Errorf("unknown portal")
				break
			}
			if p.result == nil {
				p.result, opErr = execute(p.query, false)
				if opErr != nil {
					break
				}
			}
			r := p.result
			if r.Resultset != nil && !p.completed {
				end := len(r.Values)
				if m.MaxRows > 0 && int(m.MaxRows) < end-p.pos {
					end = p.pos + int(m.MaxRows)
				}
				opErr = pgRows(b, r, p.pos, end, p.formats)
				p.pos = end
				if opErr != nil {
					break
				}
				if end < len(r.Values) {
					b.Send(&pgproto3.PortalSuspended{})
					break
				}
			}
			p.completed = true
			b.Send(&pgproto3.CommandComplete{CommandTag: []byte(pgTag(p.query, r))})
		case *pgproto3.Close:
			switch m.ObjectType {
			case 'S':
				delete(statements, m.Name)
			case 'P':
				delete(portals, m.Name)
			default:
				opErr = fmt.Errorf("invalid close target")
			}
			if opErr == nil {
				b.Send(&pgproto3.CloseComplete{})
			}
		case *pgproto3.Sync:
			failed = false
			portals = map[string]*pgPortal{}
			b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		case *pgproto3.Flush:
		default:
			opErr = fmt.Errorf("unsupported PostgreSQL message %T", msg)
		}
		if opErr != nil {
			fail(opErr)
			failed = true
		}
		if b.Flush() != nil {
			return
		}
	}
}
func formatAt(f []int16, i int) int16 {
	if len(f) == 0 {
		return 0
	}
	if len(f) == 1 {
		return f[0]
	}
	if i >= len(f) {
		return 0
	}
	return f[i]
}
func validFormats(f []int16, n int) bool {
	if len(f) != 0 && len(f) != 1 && len(f) != n {
		return false
	}
	for _, v := range f {
		if v != 0 && v != 1 {
			return false
		}
	}
	return true
}
func pgOID(f *legacy.Field) uint32 {
	switch f.Type {
	case legacy.MYSQL_TYPE_TINY:
		return 16
	case legacy.MYSQL_TYPE_LONGLONG:
		return 20
	case legacy.MYSQL_TYPE_DOUBLE:
		return 701
	default:
		return 25
	}
}
func pgDescription(r *legacy.Result, formats []int16) *pgproto3.RowDescription {
	d := &pgproto3.RowDescription{}
	for i, f := range r.Fields {
		oid := pgOID(f)
		size := int16(-1)
		if oid == 20 || oid == 701 {
			size = 8
		}
		if oid == 16 {
			size = 1
		}
		d.Fields = append(d.Fields, pgproto3.FieldDescription{Name: f.Name, DataTypeOID: oid, DataTypeSize: size, TypeModifier: -1, Format: formatAt(formats, i)})
	}
	return d
}
func pgRows(b *pgproto3.Backend, r *legacy.Result, start, end int, formats []int16) error {
	for _, row := range r.Values[start:end] {
		values := make([][]byte, len(row))
		for i, v := range row {
			var err error
			values[i], err = pgValue(v, pgOID(r.Fields[i]), formatAt(formats, i))
			if err != nil {
				return err
			}
		}
		b.Send(&pgproto3.DataRow{Values: values})
	}
	return nil
}
func pgValue(v interface{}, oid uint32, format int16) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	if format == 0 {
		return formatValue(v)
	}
	switch oid {
	case 16:
		x, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool")
		}
		if x {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	case 20:
		n, e := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		if e != nil {
			return nil, e
		}
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, uint64(n))
		return b, nil
	case 701:
		n, e := strconv.ParseFloat(fmt.Sprint(v), 64)
		if e != nil {
			return nil, e
		}
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, math.Float64bits(n))
		return b, nil
	default:
		return formatValue(v)
	}
}
func pgParameter(data []byte, oid uint32, format int16) (interface{}, error) {
	if data == nil {
		return nil, nil
	}
	if format == 0 {
		switch oid {
		case 16:
			return strconv.ParseBool(string(data))
		case 20, 21, 23:
			return strconv.ParseInt(string(data), 10, 64)
		case 700, 701:
			return strconv.ParseFloat(string(data), 64)
		default:
			return string(data), nil
		}
	}
	switch oid {
	case 16:
		if len(data) == 1 && data[0] <= 1 {
			return data[0] == 1, nil
		}
	case 20:
		if len(data) == 8 {
			return int64(binary.BigEndian.Uint64(data)), nil
		}
	case 23:
		if len(data) == 4 {
			return int32(binary.BigEndian.Uint32(data)), nil
		}
	case 21:
		if len(data) == 2 {
			return int16(binary.BigEndian.Uint16(data)), nil
		}
	case 701:
		if len(data) == 8 {
			return math.Float64frombits(binary.BigEndian.Uint64(data)), nil
		}
	case 700:
		if len(data) == 4 {
			return math.Float32frombits(binary.BigEndian.Uint32(data)), nil
		}
	case 25, 1043, 114:
		return string(data), nil
	}
	return nil, fmt.Errorf("unsupported binary parameter OID %d or invalid length", oid)
}
func pgTag(q string, r *legacy.Result) string {
	words := strings.Fields(q)
	if len(words) == 0 {
		return ""
	}
	verb := strings.ToUpper(words[0])
	if r.Resultset != nil {
		return fmt.Sprintf("SELECT %d", len(r.Values))
	}
	switch verb {
	case "INSERT", "UPSERT", "REPLACE":
		return fmt.Sprintf("INSERT 0 %d", r.AffectedRows)
	case "DELETE":
		return fmt.Sprintf("DELETE %d", r.AffectedRows)
	case "CREATE", "DROP":
		if len(words) > 1 {
			return verb + " " + strings.ToUpper(words[1])
		}
	}
	return verb
}
