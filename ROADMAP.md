# Milvus 2.6 Full-Feature Support Roadmap

[中文版](ROADMAP-cn.md)

Updated: 2026-09-18. Implementation baseline: `077de63` (PR #2).

Goal: expose **the implemented, user-facing data and administration capabilities of Milvus 2.6.23** through both MySQL and PostgreSQL protocols. Both protocols share one capability model, execution plan, and authorization model. The `mysql`, `postgres`, and `both` startup modes must continue to pass E2E tests.

This is an implementation plan. New SQL syntax, procedure names, and delivery stages below are **unimplemented proposals**, not claims of current support. See [Readme.md](Readme.md) for the currently supported syntax.

## 1. Version and Scope

- **The full-support target is pinned to 2.6.23**, the official 2.6 release baseline verified for this roadmap. Update the target through a separate compatibility PR for subsequent 2.6 patches; do not silently expand support claims. [Release notes](https://milvus.io/docs/v2.6.x/release_notes.md)
- **Keep regression coverage for existing features on 2.6.2.** This does not imply that it supports features introduced in later patches. Record each feature's minimum verified server/SDK versions, deployment requirements, and test evidence.
- Cover data models, DDL, DML, queries/searches, indexes, functions, imports, authorization, resource management, and public operational APIs. Request parameters, result types, status queries, and failure behavior are all part of each feature's scope.
- GPU, distributed resource groups, and external model services are in scope and require dedicated test environments. Mark capabilities without such validation as unverified, not complete.
- JOINs, cross-collection transactions, foreign keys, general SQL aggregation/window functions, arbitrary-expression UPDATEs, and complete MySQL/PostgreSQL system catalogs are not native Milvus capabilities. The proxy will not emulate these semantics through implicit full scans or read-modify-write operations.
- Validate compatibility with server internals such as Streaming Node, Woodpecker, Storage V2, and JSON shredding. Installation, scaling, backup/CDC tools, and cluster configuration have separate operational boundaries; they do not each require a new SQL operator.
- A proto declaration does not prove server availability. The [API inventory](docs/milvus-2.6-api-inventory.md) explicitly classifies deprecated, unimplemented, and internal replication/recovery interfaces, with evidence and handling rules. Do not silently omit them or count them as complete.

## 2. Current Foundation and Gaps

| Area | Currently implemented | Main gaps to full support |
| --- | --- | --- |
| Protocols | MySQL text/prepared; PG simple/extended; dual listeners; TLS configuration | Neutral result model, additional type encodings, cancellation, streaming results, stable error mapping |
| SDK | `milvus-sdk-go/v2` and proto at 2.3.3 | Migrate to modern `milvus/client/v2`, fill SDK gaps, and gate capabilities |
| Schema | Basic scalars, JSON, float32 vectors, primary keys/AutoID | Nullable/default values, dynamic fields, ARRAY, other vector types, struct arrays, GIS, temporal types, schema evolution |
| Administration | Subset of database/collection CRUD, manual partitions, load/release/flush | Properties, aliases, statistics, status, asynchronous tasks, partition keys, resource groups, compaction, RBAC |
| Data | Insert, full-row upsert, filtered delete | Partition routing, partial upsert, generated IDs, complete mutation counts, bulk import |
| Queries | Projection, basic filters, count, pagination, single-query dense ANN | Complete expressions, batch NQ, sparse/BM25, hybrid search, reranking, range/grouping/iterator search |
| Indexes | FLAT/HNSW/IVF_FLAT/AUTOINDEX/INVERTED | Remaining indexes, parameter matrix, state/progress, property changes, JSON/text/spatial indexes |
| Validation | 2.6.2, real executable in three modes; 90% coverage gate | Full capability validation on 2.6.23, SDK comparisons, value assertions for every type, distributed/GPU/model tests |

Evidence: [go.mod](go.mod), [execution layer](pkg/conn.go), [SQL commands](pkg/commands.go), [lifecycle tests](pkg/integration_test.go), and [E2E workflow](.github/workflows/e2e.yml). Existing capabilities still need broader parameter and result assertions and cannot be marked collectively as feature-complete.

## 3. Design Contracts

1. **One AST/plan, two protocols.** Replace the shared execution layer's MySQL result types with protocol-neutral column metadata, values, mutation results, and structured errors. Protocol adapters handle binding and encoding only.
2. **Structured parsing.** Give extensions explicit AST nodes and progressively replace lifecycle/index regex dispatch. Preserve types during binding; do not concatenate untrusted filters, JSON, or administration parameters. Bound composite values by size, dimension, and depth.
3. **SQL with explicit Milvus extensions.** Use SQL for ordinary CRUD/DDL and named `CALL milvus_*` extensions for complex searches, asynchronous tasks, and administration. Define each procedure's parameter schema, result columns, permissions, cancellation, and version requirements. Do not expose arbitrary RPC or administration passthrough.
4. **Preserve native semantics.** Distinguish SQL NULL, absent dynamic keys, and JSON null. Preserve vector precision, distance direction, NQ boundaries, and ranking. ANN grouping is not relational aggregation, and successful asynchronous submission is not task completion.
5. **Capabilities depend on both version and environment.** The proposed `SHOW MILVUS CAPABILITIES` reports server/SDK versions, capabilities, restrictions, deployment requirements, and validation status. Do not assume support when versions are unknown; use read-only probes and explicit configuration where needed.
6. **Authorization applies throughout execution.** Each session has an independent upstream identity/database context. Cross-database operations, aliases, imports, and administration procedures require authorization too. Label the current shared upstream account configuration as service-account mode; it does not provide per-user RBAC.
7. **Fill SDK gaps through controlled adapters.** Prefer a pinned official Go SDK. Encapsulate missing capabilities in narrowly scoped gRPC/REST v2 adapters that reuse identity, TLS, timeouts, and error handling. SDK omissions do not remove Milvus capabilities from scope.

## 4. Delivery Stages and Acceptance

Follow dependencies rather than committing to dates without estimates. After P0 establishes contracts, P1–P3 complete the basic data lifecycle; P4–P7 add advanced and administration capabilities; P8 completes acceptance. Identity design for P7 starts in P0 and gates the release of administration features.

### P0 — SDK, Capability Inventory, and Protocol Foundation

- [ ] Upgrade to a pinned modern Go SDK/proto; migrate session clients, schemas, indexes, mutations, and asynchronous waits individually. Preserve existing 2.6.2 regressions.
- [ ] Create a machine-readable feature registry: `id / operation / parameters / min_server / sdk_or_adapter / deployment / mysql / postgres / tests / status / exclusion_reason`.
- [ ] Extend the API inventory to parameters, schema enums, expressions, indexes, and SDK composite operations. Assign every entry a stage and implementation/test links.
- [ ] Establish neutral value/result/error models. Define SQLSTATE/error codes, parameter types, 64-bit integers, NULL, binary values, and complex encodings for both protocols.
- [ ] Define cancellation, timeouts, mutation retry boundaries, idempotency, and task status contracts. Do not automatically replay writes that may have committed before a timeout.
- [ ] Add a pinned 2.6.23 E2E target while retaining 2.6.2 regressions. Migrate before extending syntax; keep the SDK upgrade separate from the full feature expansion.

**Acceptance:** existing tests and all three E2E modes pass on both pinned versions; existing SQL behavior remains intact; unknown capabilities produce explicit errors rather than silently ignoring parameters.

### P1 — Complete Data Types and Schema

- [ ] Basic type boundaries: integer ranges, floating-point precision, VARCHAR length, JSON, INT64/VARCHAR primary keys, AutoID, and applicable properties such as `allow_insert_auto_id`.
- [ ] Nullable/default values, omitted fields, explicit NULL, and defaults. Enforce the server's type/primary-key/partition-key restrictions; do not enable defaults indiscriminately for every type.
- [ ] Dynamic field configuration, reads/writes, projection, nested paths, and schema descriptions, including server-supported enablement/evolution operations.
- [ ] ARRAY element types/max_capacity; construction, binding, and results for FloatVector, Float16Vector, BFloat16Vector, Int8Vector, BinaryVector, and SparseFloatVector.
- [ ] Arrays of Structs, their vector subfields/vector lists, nested results, and schemas required for element-level search. Do not expose the internal `ArrayOfStruct` enum as an arbitrary top-level STRUCT type.
- [ ] GEOMETRY/WKT, TIMESTAMPTZ/timezones, and their NULL, filtering, and indexing support.
- [ ] Field descriptions, analyzer/function outputs, partition keys, and clustering keys; add fields and modify supported properties while retaining errors for prohibited schema changes.

**Acceptance:** every type round-trips with value assertions through MySQL text/prepared and PG text/binary results. Cover empty arrays, empty sparse vectors, NULL, overflow, invalid WKT, timezones/DST, and dimension errors. Describe must not mutate data. Type/default restrictions follow pinned schemas and actual server behavior. [Type definitions](https://github.com/milvus-io/milvus-proto/blob/v2.6.23/proto/schema.proto)

### P2 — Databases, Collections, Partitions, and Lifecycle

- [ ] Database list/describe/create/alter/drop; property setting/removal and default inheritance; restrictions on cross-database collection access, moves, and renames.
- [ ] Collection has/list/describe/batch describe/statistics/create/rename/drop/truncate; supported properties and parameters including shards, consistency, TTL, mmap, and timezone.
- [ ] Alias create/alter/drop/list/describe; invalidate stale schema/index caches after changes and keep cross-database resolution isolated.
- [ ] Manual partition has/list/create/drop/statistics/load/release; partition keys, automatic partition counts, partition-key isolation, and clustering keys.
- [ ] Applicable load/release options for replicas, resource groups, selected fields, and skipping dynamic fields; load state/progress.
- [ ] Flush/flush-all, asynchronous status, and waits. Distinguish submitted data, durable data, and loaded/queryable data.
- [ ] Required `IF EXISTS / IF NOT EXISTS` behavior based on real existence checks and concurrent error handling, without swallowing authorization or network failures.

**Acceptance:** compare all properties and statistics with the SDK. Validate alias retargeting, session/database isolation, asynchronous failures, reloads, partition visibility after loading, and preservation of schema/indexes after truncation.

### P3 — DML and Complete Filtering/Querying

- [ ] INSERT/full UPSERT/partial UPSERT: named columns, partition selection, AutoID semantics, returned primary keys/timestamps/counts. Never fabricate exact counts unavailable from upstream.
- [ ] Partial upsert field omission, dynamic-key merging, whole-value JSON replacement, and ARRAY_APPEND/ARRAY_REMOVE. Use native Upsert requests rather than implicit proxy-side read-modify-write. [Upsert semantics](https://milvus.io/docs/v2.6.x/upsert-entities.md)
- [ ] DELETE by primary key/filter and partition; explicit full-deletion interfaces and syntax that prevents accidental deletion. Do not silently change today's rejection of unfiltered DELETE into a full deletion.
- [ ] Get-by-ID, query, count, field/dynamic-key/nested projection, aliases, and iteration; result column order, empty-result metadata, and count semantics.
- [ ] Comparison/logical/arithmetic filters, IN, LIKE, NULL, JSON/ARRAY contains/contains_all/contains_any, length/subscript/path operations, existence checks, text, GIS, and temporal expressions. Record version/type constraints per operator.
- [ ] Parameterized filter templates, Unicode, escaping, and injection boundaries; three-valued logic for absent keys, JSON null, and type mismatches.
- [ ] Strong/Bounded/Session/Eventual and public server consistency/timestamp options; session/request overrides, timestamp units, and applicability restrictions.
- [ ] Query iterator lifecycle, fetch/close/disconnection cleanup, backpressure, and bounded memory. Increasing offsets is not a substitute for a native iterator.

**Acceptance:** SDK differential tests compare every field and count. Cover filter truth tables, read-after-write, partition isolation, multiple clients, and traversal beyond a single result window. Document nontransactional concurrency semantics for partial upserts.

### P4 — Complete Vector Search

- [ ] Single/batch NQ for all vector types; results include query index, rank, primary key, and distance/score while preserving each query's boundaries.
- [ ] Search fields/partitions, topK/offset, index-specific search parameters, metric, round_decimal, ignore_growing, output fields, and dynamic fields.
- [ ] Standard/iterative filtering, range search, and search iterators; enforce metric-specific radius/range_filter directions and boundaries.
- [ ] Grouping search: group_by_field, group_size, strict_group_size, and other options supported by the pinned version. This is not general SQL GROUP BY.
- [ ] Multi-request HybridSearch with per-request ANN options/filters/candidate counts, RRF and WeightedRanker, final limit/offset, and scores. [Reranking](https://milvus.io/docs/v2.6.x/reranking.md)
- [ ] Array-of-vector/EmbeddingList, struct-array element search, supported aggregate similarity, and result locations. Searching separate vector fields individually does not complete hybrid support.
- [ ] Public search inputs such as primary-key search and advanced retrieval options supported by the pinned version. Implement them after completing the SDK/request-parameter inventory, not merely because a proto field exists.

**Acceptance:** use a FLAT exact oracle and fixed random seeds. Set explicit ANN recall thresholds and separately assert floating-point distance tolerance, tie handling, group counts, and NQ isolation. Compare hybrid scores/order with the official SDK rather than only checking nonempty results.

### P5 — Full Text, Analyzers, Functions, and Reranking

- [ ] Text-field analyzer configuration, tokenizers/filters, multilingual/multiple analyzers where supported, TEXT_MATCH, and PHRASE_MATCH; RunAnalyzer and token output options.
- [ ] BM25 functions/schemas, sparse indexes, raw-text search, sparse/dense hybrid search, and function-output read/write restrictions.
- [ ] Collection function create/add/alter/drop/describe; server-supported TextEmbedding providers/configuration, input/output field mapping, and error handling.
- [ ] Function reranking: models, decay, boost, and other types/parameters actually supported by the target version. Register these separately from hybrid RRF/weighted ranking.
- [ ] Text highlighting sources, fields, tags, fragments, and result structures; handle nulls, Unicode, and dynamic text.
- [ ] MinHash/deduplication: precomputed signatures with MINHASH_LSH/MHJACCARD supported in 2.6. **Do not promise later versions' server-side MinHash functions for 2.6.**

**Acceptance:** use a fixed corpus to verify tokens, BM25 ranking, and highlighting. Model adapters require contract tests and repeatable tests against each real provider claimed as supported. Logs must not expose provider credentials; combinations without real-provider tests remain unverified.

### P6 — Indexes and Performance Options

- [ ] Build an inventory of field type × index × metric × build/search parameters × CPU/GPU × minimum version. String passthrough alone is not support.
- [ ] Dense indexes: add IVF_SQ8, IVF_PQ, SCANN, HNSW quantization variants/refinement options, IVF_RABITQ, and other target-version capabilities, plus DISKANN and AISAQ. Preserve existing indexes.
- [ ] Binary/sparse indexes: BIN_FLAT, BIN_IVF_FLAT, MINHASH_LSH, SPARSE_INVERTED_INDEX, and applicable algorithms. Handle legacy SPARSE_WAND according to upstream deprecation policy.
- [ ] GPU indexes: GPU_CAGRA, GPU_IVF_FLAT, GPU_IVF_PQ, GPU_BRUTE_FORCE, and other entries actually registered in the target build. Document hardware/image/memory requirements.
- [ ] Scalar indexes: applicable INVERTED, BITMAP, STL_SORT, TRIE, and AUTOINDEX types; JSON path/flat/cast parameters, ARRAY, NGRAM, text matching, and GEOMETRY RTREE.
- [ ] Index create/list/describe/drop/alter, statistics/state/progress, and asynchronous build failures; public mmap/cache/warmup options and when they take effect.

**Acceptance:** reconcile enums with the pinned index registry/validators. For every legal combination, actually build, load, search/query, release, alter/drop, and verify results. Invalid combinations must fail explicitly. Indexes without GPU/disk validation cannot be complete. See [indexes and metrics](https://milvus.io/docs/v2.6.x/index-vector-fields.md); that overview is deprecated, so the final inventory must follow the pinned implementation and individual index documentation.

### P7 — Import, Authorization, Resources, and Operations

- [ ] Bulk import: native job creation/list/progress/state, file formats/storage paths/options, failure reasons, and idempotency boundaries. Audit REST v2 independently rather than relying solely on legacy gRPC Import.
- [ ] Test each format actually supported by the target version, including applicable CSV/JSON/Parquet/NumPy paths; cover databases, partitions, AutoID, nullable fields, all supported types, and post-import indexing/loading.
- [ ] Users/passwords/roles/membership, grant/revoke/list, Privilege V2, privilege groups, role descriptions, and RBAC backup/restore. Define SQL-user to Milvus-principal mapping.
- [ ] Disable administration by default; require explicit enablement and least-privilege upstream credentials. Test password changes, revocation, existing sessions, and tenant isolation. A shared root backend does not provide per-user authorization.
- [ ] Resource group create/update/describe/list/drop, node/replica transfers, and replica configuration/inspection. Verify query/streaming resource-group scope for the target deployment.
- [ ] Manual/clustering compaction, state/plans, and segment/replica/statistics APIs; separate permissions and auditing for public load-balancing actions.
- [ ] Version/health, public metrics, proxy request logs/traces/latency/errors/in-flight counts. Redact sensitive SQL parameters and correlate cancellation/timeouts with upstream requests.
- [ ] Verify public CDC/replication configuration/status contracts before exposing restricted administration procedures. Keep replication data streams, WAL dumps, and internal coordination in native tools, with documented integration boundaries.

**Acceptance:** validate both allowed and denied requests against an isolated Milvus with authentication enabled. Test object-storage import failure/recovery and resulting values. Use distributed deployments with multiple QueryNodes to verify resource groups and replica transfers. Mutations and cleanup are restricted to resources created by the tests.

### P8 — Compatibility and Release Acceptance

- [ ] Pass the capability matrix for MySQL text/binary/prepared and PG simple/extended/text/binary; preserve NULL, empty results, and complex column metadata consistently.
- [ ] PostgreSQL CancelRequest and MySQL query-cancellation paths, disconnection, statement/portal closure, connection recovery after timeout, and graceful shutdown; release resources within bounded time.
- [ ] Smoke-test real `mysql`/`psql` clients and common Go/Python/JDBC drivers. Document the minimum metadata compatibility surface separately without promising all ORMs.
- [ ] Large NQ, batch writes/results, concurrent sessions, slow clients, upstream restarts/connection failures, permission revocation, and task timeouts. Define throughput, p95/p99, and memory baselines before measuring proxy overhead.
- [ ] Complete parameter and error-path audits of the capability registry. Publish status, SQL/API mappings, minimum versions, and test links.

**Acceptance:** every in-scope entry has real E2E evidence for both protocols, no severity-P0/P1 defects remain, and exclusions have upstream evidence. Unverified dedicated-environment capabilities mean partial support; do not release a claim of full Milvus 2.6 support.

## 5. Proposed SQL Interfaces

These examples establish direction only. Finalize syntax, types, and error contracts in the corresponding implementation PR, with parser/protocol tests. The `CALL` names are not an upstream Milvus SQL standard.

```sql
-- P1/P2: extended DDL/properties; validate WITH options against the registry
CREATE TABLE docs (
    id BIGINT PRIMARY KEY,
    title VARCHAR(512),
    rating DOUBLE DEFAULT 0,
    tags ARRAY<VARCHAR(64)>,
    embedding VECTOR(3)
) WITH (enable_dynamic_field=true);
ALTER TABLE docs ADD COLUMN category VARCHAR(64) NULL;
SHOW MILVUS CAPABILITIES;

-- P3: explicit partial upsert, without implicit relational UPDATE semantics
UPSERT INTO docs (id, title) VALUES (1, 'new title')
    WITH (partial_update=true);

-- P4/P5: named, typed procedures; JSON is only the binding representation
CALL milvus_search('docs', '{"anns_field":"embedding","data":[[1,0,0]],"limit":10}');
CALL milvus_hybrid_search('docs', '<validated requests and ranker JSON>');
CALL milvus_run_analyzer('<validated analyzer and text JSON>');

-- P7: long-running tasks return native job IDs for separate status/wait calls
CALL milvus_import('docs', '<validated import request JSON>');
CALL milvus_import_status('<job id>');
CALL milvus_compact('docs', '<validated options JSON>');
```

Use `?` parameters for MySQL and `$1`, etc. for PostgreSQL. Object identifiers cannot be substituted as ordinary value parameters. Complex procedures share parameter schemas but define result column types for each protocol. Final syntax must preserve compatibility with existing `json_vector(...)`, ANN LIKE, and lifecycle commands.

## 6. CI and Definition of Done

| Layer | Planned execution | Required coverage |
| --- | --- | --- |
| Unit / fuzz | Every PR | Parser, conversions, parameter validation, plans, error mapping; `-race`, `go vet`, actionlint; statement coverage ≥90% |
| Core E2E | Every PR / main | 2.6.23 × mysql/postgres/both; existing-feature regressions on 2.6.2; real executable and isolated Milvus |
| Feature E2E | Each feature PR; full scheduled suite | Positive/negative cases for every type/index/filter/search parameter; official SDK comparisons; explicit rejection on unsupported versions |
| Distributed / import / RBAC | Required for relevant PRs; full scheduled suite | Multiple nodes, object storage, real authentication, revocation, asynchronous failures, resource transfers |
| GPU / provider / load | Dedicated runners; required before releasing relevant features | Each claimed hardware/model combination, load, and fault validation |

These workflow extensions are not implemented yet; current tests still use 2.6.2 as described in the README. Every stage must deliver implementation, SQL examples, permission/version restrictions, result contracts for both protocols, unit tests, real E2E tests, review, and CI evidence.

Tests must separately verify values, field types, NULL/absence, primary keys, counts, ordering/distances, and status. Success without errors or nonempty results is insufficient. Poll task states with deadlines; fixed sleeps do not prove completion. Upload redacted logs/versions/configuration on failure, and clean up only uniquely named test-owned resources.

Measure completion against registered capabilities and parameter contracts, not code coverage or mapped RPC counts. Skipped dedicated-environment tests, mock-only success, or implementation of just one protocol do not meet acceptance.

## 7. Initial PR Breakdown

| Order | Independently reviewable delivery | Prerequisites |
| --- | --- | --- |
| 1 | Initial feature registry, pinned SDK/API parameter inventory, 2.6.23 E2E baseline | This roadmap |
| 2 | Modern Go SDK/adapter migration preserving existing SQL contracts | 1 |
| 3 | Neutral result/error types and binding/NULL/complex-type encoding foundation | 2 |
| 4 | Nullable/default values and ARRAY/dynamic fields with value-level E2E assertions | 3 |
| 5 | Sparse/low-precision/binary vectors and corresponding basic indexes | 3 |
| 6 | Collection properties, aliases, schema evolution, partition routing | 2–4 |
| 7 | Partial upserts, complete filter templates, consistency | 4, 6 |
| 8 | Batch/range/iterator/grouping and hybrid search | 5, 7 |
| 9 | BM25/analyzers/functions/reranking and advanced types/indexes in separate batches | Relevant P1/P4 foundations |
| 10 | Identity mapping and RBAC before enabling import/resource/compaction administration; final P8 acceptance | P0 identity design, P2 |

Split subsequent PRs by capability family, keeping implementation and E2E tests together. Establish core semantics and interfaces before expanding the number of indexes/functions so incorrect data-type or identity models do not spread.
