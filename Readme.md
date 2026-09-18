# Milvus SQL Proxy

Connect MySQL or PostgreSQL clients to Milvus using a shared SQL execution layer.
Both protocols can run together; each connection owns its own Milvus client and
selected database. This is a Milvus adapter, not a relational database: joins,
transactions and arbitrary SQL expressions are not supported.

## Run

Requires Go 1.23+ and Milvus (integration-tested with 2.6.2).

```sh
go run ./cmd -config config.yaml
mysql -h 127.0.0.1 -P 3306 -u root default
psql 'postgresql://root@127.0.0.1:5432/default?sslmode=disable'
```

`mode` is `mysql`, `postgres`, or `both`; omitted mode preserves the original
MySQL-only behavior. `addr` is the MySQL listener and `postgresAddr` is the
PostgreSQL listener. The sample runs both, bound to loopback. Set `user` and
`password` to authenticate SQL clients; these are separate from Milvus credentials.
For network access, configure a password and frontend `tlsCert`/`tlsKey`, and
require TLS in clients. PostgreSQL uses password authentication; without TLS the
password travels in cleartext. The proxy does not expose per-user Milvus RBAC.
`milvus.tlsSecure` enables TLS to Milvus; HTTPS endpoints also enable it.

Commands time out after `queryTimeoutSeconds` (default 30). Increase this for
large synchronous index builds, load, or flush operations. A timed-out Milvus
mutation may have been accepted; inspect its state before retrying.

## Supported SQL

The same SQL subset is available on both ports. MySQL text and prepared/binary
queries are supported. PostgreSQL simple and extended queries (Parse, Bind,
Describe, Execute, Sync), `$n` parameters, text/binary scalar values and quoted
identifiers are supported, including pgx's default prepared-statement mode.
This does not emulate PostgreSQL system catalogs, ORM migrations or all dialect
syntax. MySQL uses `?` parameters; PostgreSQL uses `$1`, `$2`, etc.

| Area | Commands |
| --- | --- |
| Databases | `SHOW DATABASES`, `CREATE DATABASE db`, `USE db`, `DROP DATABASE db` |
| Collections | `SHOW TABLES`, `CREATE TABLE`, `DESCRIBE table`, `DROP TABLE` |
| Indexes | `CREATE INDEX`, `SHOW INDEXES FROM table`, `DROP INDEX name ON table` |
| Memory/storage | `LOAD TABLE table`, `RELEASE TABLE table`, `FLUSH TABLE table` |
| Partitions | `CREATE/DROP/LOAD/RELEASE PARTITION name ON table`, `SHOW PARTITIONS FROM table` |
| Writes | Multi-row `INSERT`, full-row `UPSERT` (also `REPLACE`), filtered `DELETE` |
| Reads | Field projection, `*`, scalar filters, `count(*)`, pagination, dense vector ANN |

Field types: `BOOL`/`BOOLEAN` (also `TINYINT(1)`), `TINYINT`, `SMALLINT`, `INT`,
`BIGINT`, `FLOAT`, `DOUBLE`, `VARCHAR(n)`, `JSON`, `VECTOR(dim)` (float32).
Collections need one inline `BIGINT` or `VARCHAR` primary key and at least one
vector field. Auto-ID uses `BIGINT AUTO_INCREMENT PRIMARY KEY`. All non-auto-ID
fields must be supplied; SQL NULL/nullable fields and default values are not
supported. Upsert requires an explicit primary key; auto-ID upserts are rejected.
Vectors are JSON numeric arrays; dimensions and every row are validated before
sending a write. JSON columns accept a string containing valid JSON.

```sql
CREATE DATABASE demo;
USE demo;
CREATE TABLE documents (
    id BIGINT PRIMARY KEY,
    title VARCHAR(256),
    published BOOLEAN,
    embedding VECTOR(3)
);
CREATE INDEX embedding_idx ON documents (embedding)
    USING HNSW WITH (metric_type='COSINE', M=16, efConstruction=200);
INSERT INTO documents VALUES
    (1, 'first', true, json_vector('[1,0,0]')),
    (2, 'second', false, json_vector('[0,1,0]'));
LOAD TABLE documents;

SELECT id, title FROM documents WHERE published=true AND id IN (1,2) LIMIT 10;
SELECT count(*) FROM documents WHERE id>=1;
SELECT id, title, _distance FROM documents
    WHERE embedding LIKE json_vector('[1,0,0]') AND published=true LIMIT 3;
UPSERT INTO documents VALUES (2, 'updated', true, json_vector('[0,0,1]'));
DELETE FROM documents WHERE id=2;

RELEASE TABLE documents;
DROP INDEX embedding_idx ON documents;
DROP TABLE documents;
USE `default`; -- PostgreSQL: USE "default";
DROP DATABASE demo;
```

Create does **not** implicitly index or load. Index methods: `FLAT`, `HNSW`,
`IVF_FLAT`, `AUTOINDEX`, scalar `INVERTED`. Dense metrics: `L2` (default), `IP`,
`COSINE`. Search uses the field's actual index metric, returns nearest neighbors
in Milvus order and optionally the reserved `_distance` (distance/similarity according to the
metric). Only one positive vector predicate is allowed, optionally combined with
scalar predicates using `AND`; vector predicates inside `OR` or `NOT` are rejected.
Inserts/upserts target the default partition. Query/search/delete have no partition
selector; they follow Milvus behavior across the collection (searching loaded partitions).

Scalar predicates: `=`, `!=`, `<>`, `<`, `<=`, `>`, `>=`, `IN`, `NOT IN`, `LIKE`,
`AND`, `OR`, `NOT`, parentheses. `LIMIT count OFFSET offset` and `LIMIT offset,count`
are supported; default limit 100, maximum limit + offset 16384. `LIMIT 0` returns
metadata and no rows. Count has no pagination. Query/search use strong consistency.
SDK v2 does not return deleted row count, so DELETE reports 0 affected rows even
when matching entities were removed.

Unsupported clauses return errors: aliases, joins, qualified collection names,
DISTINCT, GROUP BY, HAVING, ORDER BY, transactions, UPDATE, NULL predicates,
IF EXISTS / IF NOT EXISTS, schema alterations, multi-statement requests and
arbitrary functions. Database selection is through startup database or `USE`.
Sparse/binary vectors, hybrid search/reranking, RBAC and bulk import are outside
this SQL subset. PostgreSQL cancellation packets are not supported; server query
timeouts and connection/server shutdown bound execution.

## Test

```sh
go test -race ./...
go vet ./...
# Point only at a disposable test Milvus; the test creates/removes its own databases.
MILVUS_TEST_ADDR=localhost:19530 go test -race ./pkg -run TestMilvusIntegration -v
```

CI requires at least 90% statement coverage and runs the lifecycle through both `database/sql` MySQL and pgx clients against a
standalone Milvus container, including authentication failures and rejected SQL.
Unit tests use isolated session mocks and real local protocol listeners.

## Credits

SQL grammar: [haorenfsa/sqlparser](https://github.com/haorenfsa/sqlparser), forked
from [xwb1989/sqlparser](https://github.com/xwb1989/sqlparser). MySQL protocol:
[go-mysql](https://github.com/go-mysql-org/go-mysql). PostgreSQL protocol:
[pgx](https://github.com/jackc/pgx). Original result helpers derive from
[kingshard](https://github.com/flike/kingshard).

### Executable E2E workflow

The separate `e2e` workflow runs on pull requests, pushes to `main`, and manual
dispatch. Its `mysql`, `postgres`, and `both` matrix builds the real proxy with
race detection, starts Milvus 2.6.2, and launches the proxy through a generated
configuration file. Each mode exercises the complete SQL lifecycle through
network clients, rejects bad credentials, checks inactive listeners, and verifies
clean SIGTERM shutdown and released ports. Proxy/Milvus logs and test output are
uploaded as `e2e-<mode>` artifacts, including on failure.

Run one mode locally against a **disposable** Milvus:

```sh
go build -race -o /tmp/milvus-sql-proxy ./cmd
MILVUS_TEST_ADDR=localhost:19530 E2E_PROXY_BINARY=/tmp/milvus-sql-proxy \
  E2E_MODE=both go test -race ./pkg -run '^TestBinaryE2E$' -count=1 -timeout=5m -v
```

Set `E2E_ARTIFACT_DIR` to retain local diagnostics; otherwise tests use their
temporary directory. Tests only remove the uniquely named databases they create.
