package pkg

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/flike/kingshard/core/golog"
	"github.com/flike/kingshard/mysql"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"github.com/pkg/errors"
	"github.com/xwb1989/sqlparser"
	"github.com/xwb1989/sqlparser/dependency/querypb"
)

func (c *ClientConn) handleDDL(stmt *sqlparser.DDL, args []interface{}) error {
	switch stmt.Action {
	case sqlparser.CreateStr:
		return c.handleCreateTable(stmt, args)
	case sqlparser.DropStr:
		return c.handleDropTable(stmt, args)
	default:
		return mysql.NewError(mysql.ER_UNKNOWN_ERROR, fmt.Sprintf("ddl %s not supported", stmt.Action))
	}
}

func DDLToMilvusSchema(stmt *sqlparser.DDL) (*MilvusSchema, error) {
	ret := new(MilvusSchema)
	ret.Schema = new(entity.Schema)
	schema := ret.Schema
	golog.Info("ddl", "DDLToMilvusSchema", "stmt", 0, stmt.NewName.Name.String())
	schema.CollectionName = stmt.NewName.Name.String()
	if stmt.TableSpec == nil {
		return nil, errors.Errorf("table spec is nil")
	}
	if !stmt.NewName.Qualifier.IsEmpty() || stmt.TableSpec.Options != "" || len(stmt.TableSpec.Indexes) > 0 {
		return nil, errors.New("qualified tables, table options and table-level constraints are not supported; use inline PRIMARY KEY and CREATE INDEX")
	}
	schema.Description = ""
	schema.Fields = make([]*entity.Field, 0, len(stmt.TableSpec.Columns))
	for _, col := range stmt.TableSpec.Columns {
		field, err := columnToMilvusField(col)
		if err != nil {
			return nil, errors.Wrapf(err, "column[%s] to milvus field failed", col.Name.String())
		}
		schema.Fields = append(schema.Fields, field)
	}
	seen := map[string]bool{}
	pk := 0
	vectors := 0
	for _, f := range schema.Fields {
		if seen[f.Name] {
			return nil, errors.New("duplicate field")
		}
		seen[f.Name] = true
		if f.Name == "_distance" {
			return nil, errors.New("_distance is reserved for vector search scores")
		}
		if !fieldIdentifier.MatchString(f.Name) {
			return nil, errors.New("invalid field name")
		}
		if f.PrimaryKey {
			pk++
			if f.DataType != entity.FieldTypeInt64 && f.DataType != entity.FieldTypeVarChar {
				return nil, errors.New("primary key must be BIGINT or VARCHAR")
			}
			if f.AutoID && f.DataType != entity.FieldTypeInt64 {
				return nil, errors.New("auto-ID requires BIGINT")
			}
		}
		if f.DataType == entity.FieldTypeFloatVector {
			vectors++
		}
	}
	if pk != 1 || vectors == 0 {
		return nil, errors.New("collection requires exactly one primary key and at least one vector field")
	}
	ret.ShardNum = 1
	// TODO: shard num
	// default: ret.ShardNum = 2
	return ret, nil
}

func columnToMilvusField(col *sqlparser.ColumnDefinition) (*entity.Field, error) {
	field := new(entity.Field)
	field.Name = col.Name.String()
	if col.Type.Default != nil || col.Type.OnUpdate != nil || col.Type.Unsigned || col.Type.Zerofill || col.Type.Scale != nil || col.Type.Charset != "" || col.Type.Collate != "" {
		return nil, errors.New("unsupported column options")
	}

	var supportType bool
	if strings.EqualFold(col.Type.Type, "bool") || strings.EqualFold(col.Type.Type, "boolean") || (col.Type.Type == "tinyint" && col.Type.Length != nil && string(col.Type.Length.Val) == "1") {
		field.DataType = entity.FieldTypeBool
	} else if strings.EqualFold(col.Type.Type, "json") {
		field.DataType = entity.FieldTypeJSON
	} else if col.Type.Type == sqlparser.KeywordString(sqlparser.VECTOR) {
		field.DataType = entity.FieldTypeFloatVector
		if col.Type.Length == nil {
			return nil, errors.Errorf("vector dim is nil")
		}
		golog.Debug("ddl", "columnToMilvusField", "dim", 0, string(col.Type.Length.Val))
		dim, err := strconv.Atoi(string(col.Type.Length.Val))
		if err != nil || dim < 1 || dim > 32768 {
			return nil, errors.New("vector dimension must be 1..32768")
		}
		field.TypeParams = map[string]string{
			"dim": string(col.Type.Length.Val),
		}
	} else {
		field.DataType, supportType = MilvusDataTypeMap[col.Type.SQLType()]
		if !supportType {
			return nil, errors.Errorf("type[%s] not supported", col.Type.SQLType())
		}
		if field.DataType == entity.FieldTypeVarChar {
			if col.Type.Length == nil {
				return nil, errors.Errorf("varchar max_length must be specified")
			}
			max, err := strconv.Atoi(string(col.Type.Length.Val))
			if err != nil || max < 1 || max > 65535 {
				return nil, errors.New("varchar length must be 1..65535")
			}
			field.TypeParams = map[string]string{
				"max_length": string(col.Type.Length.Val),
			}
		}
	}

	switch int(col.Type.KeyOpt) {
	case colKeyPrimary:
		field.PrimaryKey = true
		if col.Type.Autoincrement {
			field.AutoID = true
		}
	case colKeyNone:
		if col.Type.Autoincrement {
			return nil, errors.Errorf("not primarykey, autoincrement not supported")
		}
	default:
		return nil, errors.Errorf("key option[%d] not supported", col.Type.KeyOpt)
	}
	if col.Type.Comment != nil {
		field.Description = string(col.Type.Comment.Val)
	}
	return field, nil
}

const (
	colKeyNone = iota
	colKeyPrimary
	colKeySpatialKey
	colKeyUnique
	colKeyUniqueKey
	colKey
)

var MilvusDataTypeMap = map[querypb.Type]entity.FieldType{
	querypb.Type_INT8:    entity.FieldTypeInt8,
	querypb.Type_INT16:   entity.FieldTypeInt16,
	querypb.Type_INT32:   entity.FieldTypeInt32,
	querypb.Type_INT64:   entity.FieldTypeInt64,
	querypb.Type_FLOAT32: entity.FieldTypeFloat,
	querypb.Type_FLOAT64: entity.FieldTypeDouble,
	querypb.Type_TEXT:    entity.FieldTypeString,
	querypb.Type_VARCHAR: entity.FieldTypeVarChar,
	// convert
	querypb.Type_DATE:     entity.FieldTypeInt32,
	querypb.Type_DATETIME: entity.FieldTypeInt64,
}

func (c *ClientConn) handleCreateTable(stmt *sqlparser.DDL, args []interface{}) error {
	milvusSchema, err := DDLToMilvusSchema(stmt)
	if err != nil {
		return mysql.NewError(mysql.ER_CANT_CREATE_TABLE, err.Error())
	}
	golog.Info("ddl", "handleCreateTable", "CreateCollection", 0)
	// TODO: consistency level
	err = c.upstream.CreateCollection(c.ctx, milvusSchema.Schema, milvusSchema.ShardNum, client.WithConsistencyLevel(entity.ClStrong))
	if err != nil {
		return mysql.NewError(mysql.ER_CANT_CREATE_TABLE, err.Error())
	}

	return c.writeOK(nil)
}

func (c *ClientConn) handleDropTable(stmt *sqlparser.DDL, args []interface{}) error {
	golog.Info("ddl", "handleDropTable", "DropCollection", 0, stmt.Table.Name.String())
	if !stmt.Table.Qualifier.IsEmpty() || stmt.IfExists {
		return fmt.Errorf("qualified DROP and IF EXISTS are unsupported")
	}
	err := c.upstream.DropCollection(c.ctx, stmt.Table.Name.String())
	if err != nil {
		return mysql.NewError(mysql.ER_CANT_DROP_FIELD_OR_KEY, err.Error())
	}
	return c.writeOK(nil)
}
