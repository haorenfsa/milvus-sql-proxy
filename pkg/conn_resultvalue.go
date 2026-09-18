// Copyright 2016 The kingshard Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package pkg

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/flike/kingshard/core/errors"
	"github.com/flike/kingshard/core/hack"
	"github.com/flike/kingshard/mysql"
)

func formatValue(value interface{}) ([]byte, error) {
	if value == nil {
		return hack.Slice("NULL"), nil
	}
	switch v := value.(type) {
	case bool:
		return strconv.AppendBool(nil, v), nil
	case int8:
		return strconv.AppendInt(nil, int64(v), 10), nil
	case int16:
		return strconv.AppendInt(nil, int64(v), 10), nil
	case int32:
		return strconv.AppendInt(nil, int64(v), 10), nil
	case int64:
		return strconv.AppendInt(nil, int64(v), 10), nil
	case int:
		return strconv.AppendInt(nil, int64(v), 10), nil
	case uint8:
		return strconv.AppendUint(nil, uint64(v), 10), nil
	case uint16:
		return strconv.AppendUint(nil, uint64(v), 10), nil
	case uint32:
		return strconv.AppendUint(nil, uint64(v), 10), nil
	case uint64:
		return strconv.AppendUint(nil, uint64(v), 10), nil
	case uint:
		return strconv.AppendUint(nil, uint64(v), 10), nil
	case float32:
		return strconv.AppendFloat(nil, float64(v), 'f', -1, 64), nil
	case float64:
		return strconv.AppendFloat(nil, float64(v), 'f', -1, 64), nil
	case []float32:
		return json.Marshal(v)
	case []byte:
		return v, nil
	case string:
		return hack.Slice(v), nil
	default:
		return nil, fmt.Errorf("invalid type %T", value)
	}
}

func formatField(field *mysql.Field, value interface{}) error {
	switch value.(type) {
	case nil:
		field.Type = mysql.MYSQL_TYPE_NULL
		return nil
	case bool:
		field.Charset = 63
		field.Type = mysql.MYSQL_TYPE_TINY
	case int8, int16, int32, int64, int:
		field.Charset = 63
		field.Type = mysql.MYSQL_TYPE_LONGLONG
		field.Flag = mysql.BINARY_FLAG | mysql.NOT_NULL_FLAG
	case uint8, uint16, uint32, uint64, uint:
		field.Charset = 63
		field.Type = mysql.MYSQL_TYPE_LONGLONG
		field.Flag = mysql.BINARY_FLAG | mysql.NOT_NULL_FLAG | mysql.UNSIGNED_FLAG
	case float32, float64:
		field.Charset = 63
		field.Type = mysql.MYSQL_TYPE_DOUBLE
		field.Flag = mysql.BINARY_FLAG | mysql.NOT_NULL_FLAG
	case string, []byte, []float32:
		field.Charset = 33
		field.Type = mysql.MYSQL_TYPE_VAR_STRING
	default:
		return fmt.Errorf("unsupport type %T for resultset", value)
	}
	return nil
}

// Result construction is transport-neutral: adapters encode rows on the wire.
func (c *ClientConn) buildResultset(fields []*mysql.Field, names []string, values [][]interface{}) (*mysql.Resultset, error) {
	if len(fields) != 0 && len(fields) != len(names) {
		return nil, errors.ErrInvalidArgument
	}
	r := newEmptyResultset(names)
	if len(fields) > 0 {
		r.Fields = fields
	}
	for i, row := range values {
		if len(row) != len(names) {
			return nil, fmt.Errorf("row %d has %d columns, expected %d", i, len(row), len(names))
		}
		if i == 0 && len(fields) == 0 {
			for j, v := range row {
				if err := formatField(r.Fields[j], v); err != nil {
					return nil, err
				}
			}
		}
	}
	r.Values = values
	return r, nil
}

func (c *ClientConn) writeResultset(status uint16, r *mysql.Resultset) error {
	c.result = &mysql.Result{Resultset: r, Status: status}
	return nil
}

func newEmptyResultset(fields []string) *mysql.Resultset {
	r := new(mysql.Resultset)
	r.Fields = make([]*mysql.Field, len(fields))
	r.FieldNames = make(map[string]int, len(fields))
	for i := range fields {
		r.Fields[i] = &mysql.Field{}
		r.Fields[i].Name = hack.Slice(fields[i])
		r.Fields[i].Type = mysql.MYSQL_TYPE_VAR_STRING
		r.Fields[i].Charset = 33
		r.FieldNames[fields[i]] = i
	}

	r.Values = make([][]interface{}, 0)
	r.RowDatas = make([]mysql.RowData, 0)

	return r
}
