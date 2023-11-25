package pkg

import (
	"fmt"

	"github.com/flike/kingshard/core/golog"
	"github.com/flike/kingshard/mysql"
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

type MilvusSchema struct {
	*entity.Schema
	ShardNum int32
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
	schema.Description = stmt.TableSpec.Options
	schema.Fields = make([]*entity.Field, 0, len(stmt.TableSpec.Columns))
	for _, col := range stmt.TableSpec.Columns {
		field, err := columnToMilvusField(col)
		if err != nil {
			return nil, errors.Wrapf(err, "column[%s] to milvus field failed", col.Name.String())
		}
		schema.Fields = append(schema.Fields, field)
	}
	// TODO: shard num
	// default: ret.ShardNum = 2
	return ret, nil
}

func columnToMilvusField(col *sqlparser.ColumnDefinition) (*entity.Field, error) {
	field := new(entity.Field)
	field.Name = col.Name.String()
	var supportType bool
	if col.Type.Type == sqlparser.KeywordString(sqlparser.VECTOR) {
		field.DataType = entity.FieldTypeFloatVector
		if col.Type.Length == nil {
			return nil, errors.Errorf("vector dim is nil")
		}
		golog.Debug("ddl", "columnToMilvusField", "dim", 0, string(col.Type.Length.Val))
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
	err = c.upstream.CreateCollection(c.ctx, milvusSchema.Schema, milvusSchema.ShardNum)
	if err != nil {
		return mysql.NewError(mysql.ER_CANT_CREATE_TABLE, err.Error())
	}
	return c.writeOK(nil)
}

func (c *ClientConn) handleDropTable(stmt *sqlparser.DDL, args []interface{}) error {
	err := c.upstream.DropCollection(c.ctx, stmt.Table.Name.String())
	if err != nil {
		return mysql.NewError(mysql.ER_CANT_DROP_FIELD_OR_KEY, err.Error())
	}
	return c.writeOK(nil)
}
