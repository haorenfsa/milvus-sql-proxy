package pkg

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const ident = "([A-Za-z_][A-Za-z0-9_]*)"

var lifecycleRE = regexp.MustCompile(`(?i)^(LOAD|RELEASE|FLUSH) (?:TABLE|COLLECTION) ` + ident + `$`)
var describeRE = regexp.MustCompile(`(?i)^(?:DESCRIBE|DESC) ` + ident + `$`)
var createIndexRE = regexp.MustCompile(`(?i)^CREATE INDEX ` + ident + ` ON ` + ident + `\s*\(\s*` + ident + `\s*\) USING (FLAT|IVF_FLAT|HNSW|AUTOINDEX|INVERTED)(?: WITH\s*\((.*)\))?$`)
var dropIndexRE = regexp.MustCompile(`(?i)^DROP INDEX ` + ident + ` ON ` + ident + `$`)
var showIndexesRE = regexp.MustCompile(`(?i)^SHOW INDEXES FROM ` + ident + `$`)
var partitionRE = regexp.MustCompile(`(?i)^(CREATE|DROP|LOAD|RELEASE) PARTITION ` + ident + ` ON ` + ident + `$`)
var showPartitionsRE = regexp.MustCompile(`(?i)^SHOW PARTITIONS FROM ` + ident + `$`)
var upsertRE = regexp.MustCompile(`(?i)^UPSERT\s+INTO\b`)

type sqlIndex struct {
	name, kind string
	params     map[string]string
}

func (i sqlIndex) Name() string                { return i.name }
func (i sqlIndex) IndexType() entity.IndexType { return entity.IndexType(i.kind) }
func (i sqlIndex) Params() map[string]string   { return i.params }

func (c *ClientConn) rows(names []string, rows [][]interface{}) error {
	r, e := c.buildResultset(nil, names, rows)
	if e != nil {
		return e
	}
	return c.writeResultset(c.status, r)
}
func (c *ClientConn) handleMilvusCommand(sql string) (bool, error) {
	sql = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sql), ";"))
	if upsertRE.MatchString(sql) {
		if c.describe {
			return true, c.writeOK(nil)
		}
		return true, c.handleQuery(upsertRE.ReplaceAllString(sql, "REPLACE INTO"))
	}
	if m := lifecycleRE.FindStringSubmatch(sql); m != nil {
		if c.describe {
			return true, c.writeOK(nil)
		}
		var e error
		switch strings.ToUpper(m[1]) {
		case "LOAD":
			e = c.upstream.LoadCollection(c.ctx, m[2], false)
		case "RELEASE":
			e = c.upstream.ReleaseCollection(c.ctx, m[2])
		case "FLUSH":
			e = c.upstream.Flush(c.ctx, m[2], false)
		}
		if e != nil {
			return true, e
		}
		return true, c.writeOK(nil)
	}
	if m := describeRE.FindStringSubmatch(sql); m != nil {
		names := []string{"Field", "Type", "PrimaryKey", "AutoID", "Parameters"}
		if c.describe {
			r, _ := c.buildResultset(nil, names, nil)
			r.Fields[2] = resultField(names[2], entity.FieldTypeBool)
			r.Fields[3] = resultField(names[3], entity.FieldTypeBool)
			return true, c.writeResultset(c.status, r)
		}
		schema, e := c.GetCollectinSchema(m[1])
		if e != nil {
			return true, e
		}
		rows := [][]interface{}{}
		for _, f := range schema.Fields {
			params, _ := json.Marshal(f.TypeParams)
			rows = append(rows, []interface{}{f.Name, f.DataType.String(), f.PrimaryKey, f.AutoID, string(params)})
		}
		return true, c.rows(names, rows)
	}
	if m := createIndexRE.FindStringSubmatch(sql); m != nil {
		if c.describe {
			return true, c.writeOK(nil)
		}
		params := map[string]string{"index_type": strings.ToUpper(m[4])}
		if m[5] != "" {
			for _, part := range strings.Split(m[5], ",") {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) != 2 {
					return true, fmt.Errorf("index options must be key=value")
				}
				key := strings.TrimSpace(kv[0])
				switch key {
				case "metric_type", "M", "efConstruction", "nlist":
				default:
					return true, fmt.Errorf("unsupported index parameter %s", key)
				}
				if _, ok := params[key]; ok {
					return true, fmt.Errorf("duplicate index parameter")
				}
				params[key] = strings.Trim(strings.TrimSpace(kv[1]), "'\"")
			}
		}
		if params["index_type"] != "INVERTED" {
			if params["metric_type"] == "" {
				params["metric_type"] = "L2"
			}
			switch params["metric_type"] {
			case "L2", "IP", "COSINE":
			default:
				return true, fmt.Errorf("metric_type must be L2, IP or COSINE")
			}
		}
		switch params["index_type"] {
		case "HNSW":
			if params["M"] == "" {
				params["M"] = "16"
			}
			if params["efConstruction"] == "" {
				params["efConstruction"] = "200"
			}
		case "IVF_FLAT":
			if params["nlist"] == "" {
				params["nlist"] = "128"
			}
		}
		idx, err := buildIndex(params)
		if err != nil {
			return true, err
		}
		e := c.upstream.CreateIndex(c.ctx, m[2], m[3], sqlIndex{m[1], params["index_type"], idx.Params()}, false, client.WithIndexName(m[1]))
		if e != nil {
			return true, e
		}
		return true, c.writeOK(nil)
	}
	if m := dropIndexRE.FindStringSubmatch(sql); m != nil {
		if c.describe {
			return true, c.writeOK(nil)
		}
		e := c.upstream.DropIndex(c.ctx, m[2], "", client.WithIndexName(m[1]))
		if e != nil {
			return true, e
		}
		return true, c.writeOK(nil)
	}
	if m := showIndexesRE.FindStringSubmatch(sql); m != nil {
		names := []string{"Name", "Type", "Parameters"}
		if c.describe {
			return true, c.rows(names, nil)
		}
		indexes, e := c.listIndexes(m[1])
		if e != nil {
			return true, e
		}
		rows := [][]interface{}{}
		for _, idx := range indexes {
			params, _ := json.Marshal(idx.Params())
			rows = append(rows, []interface{}{idx.Name(), string(idx.IndexType()), string(params)})
		}
		return true, c.rows(names, rows)
	}
	if m := partitionRE.FindStringSubmatch(sql); m != nil {
		if c.describe {
			return true, c.writeOK(nil)
		}
		var e error
		switch strings.ToUpper(m[1]) {
		case "CREATE":
			e = c.upstream.CreatePartition(c.ctx, m[3], m[2])
		case "DROP":
			e = c.upstream.DropPartition(c.ctx, m[3], m[2])
		case "LOAD":
			e = c.upstream.LoadPartitions(c.ctx, m[3], []string{m[2]}, false)
		case "RELEASE":
			e = c.upstream.ReleasePartitions(c.ctx, m[3], []string{m[2]})
		}
		if e != nil {
			return true, e
		}
		return true, c.writeOK(nil)
	}
	if m := showPartitionsRE.FindStringSubmatch(sql); m != nil {
		names := []string{"Partition"}
		if c.describe {
			return true, c.rows(names, nil)
		}
		ps, e := c.upstream.ShowPartitions(c.ctx, m[1])
		if e != nil {
			return true, e
		}
		rows := [][]interface{}{}
		for _, p := range ps {
			rows = append(rows, []interface{}{p.Name})
		}
		return true, c.rows(names, rows)
	}
	return false, nil
}

func buildIndex(params map[string]string) (entity.Index, error) {
	kind := params["index_type"]
	metric := entity.MetricType(params["metric_type"])
	allowed := map[string]bool{"index_type": true, "metric_type": kind != "INVERTED"}
	switch kind {
	case "HNSW":
		allowed["M"] = true
		allowed["efConstruction"] = true
	case "IVF_FLAT":
		allowed["nlist"] = true
	}
	for key := range params {
		if !allowed[key] {
			return nil, fmt.Errorf("parameter %s is not valid for %s", key, kind)
		}
	}
	switch kind {
	case "FLAT":
		return entity.NewIndexFlat(metric)
	case "HNSW":
		m, e := strconv.Atoi(params["M"])
		if e != nil {
			return nil, e
		}
		ef, e := strconv.Atoi(params["efConstruction"])
		if e != nil {
			return nil, e
		}
		return entity.NewIndexHNSW(metric, m, ef)
	case "IVF_FLAT":
		n, e := strconv.Atoi(params["nlist"])
		if e != nil {
			return nil, e
		}
		return entity.NewIndexIvfFlat(metric, n)
	case "AUTOINDEX":
		return entity.NewIndexAUTOINDEX(metric)
	case "INVERTED":
		return entity.NewGenericIndex("", entity.IndexType(kind), map[string]string{"index_type": kind, "params": "{}"}), nil
	default:
		return nil, fmt.Errorf("unsupported index type %s", kind)
	}
}
