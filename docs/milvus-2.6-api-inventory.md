# Milvus 2.6.23 API Inventory

[中文版](milvus-2.6-api-inventory-cn.md)

Updated: 2026-09-18. Companion to the [roadmap](../ROADMAP.md).

This is a **scope and gap inventory**, not an implementation checklist marked complete. It groups all 117 RPC declarations in the pinned `MilvusService`, without missing or duplicate names. Classification follows public purpose, server implementation, and project boundaries. Proto-only, deprecated, and internal interfaces must not be treated as ordinary SQL capabilities awaiting implementation.

Source evidence:

- [milvus.proto v2.6.23](https://github.com/milvus-io/milvus-proto/blob/v2.6.23/proto/milvus.proto): complete RPC request/response declarations.
- [schema.proto v2.6.23](https://github.com/milvus-io/milvus-proto/blob/v2.6.23/proto/schema.proto): types, functions, fields, and search result structures.
- [Proxy implementation v2.6.23](https://github.com/milvus-io/milvus/blob/v2.6.23/internal/proxy/impl.go): actual implementations and deprecated/unimplemented responses.
- [Proxy service v2.6.23](https://github.com/milvus-io/milvus/blob/v2.6.23/internal/distributed/proxy/service.go): exposed entry points and the unimplemented server base.
- [REST v2 handler v2.6.23](https://github.com/milvus-io/milvus/blob/v2.6.23/internal/distributed/proxy/httpserver/handler_v2.go): additional REST entry points; P0 must extract the complete routes and request fields.

## RPC-to-Stage Mapping

| Capability family | Stage/boundary | RPCs | Current status and remaining work |
| --- | --- | --- | --- |
| Database | P2 | `ListDatabases`, `CreateDatabase`, `DropDatabase`, `DescribeDatabase`, `AlterDatabase` | List/create/drop subset exists; add describe/alter, properties, and cross-database behavior. |
| Collection | P1/P2 | `CreateCollection`, `DropCollection`, `HasCollection`, `DescribeCollection`, `BatchDescribeCollection`, `ShowCollections`, `GetCollectionStatistics`, `AlterCollection`, `AlterCollectionField`, `AddCollectionField`, `RenameCollection`, `TruncateCollection` | Basic create/drop/list/describe exists; add remaining operations and complete schema/property parameters. |
| Function DDL | P5 | `AddCollectionFunction`, `AlterCollectionFunction`, `DropCollectionFunction` | Planned; function schemas at collection creation are also covered by P1/P5. |
| Partition | P2/P3 | `CreatePartition`, `DropPartition`, `HasPartition`, `ShowPartitions`, `GetPartitionStatistics` | Create/drop/list exists; add statistics, existence checks, routing, and partition-key semantics. |
| Load | P2 | `LoadCollection`, `ReleaseCollection`, `LoadPartitions`, `ReleasePartitions`, `GetLoadingProgress`, `GetLoadState` | Synchronous load/release subset exists; add parameters, asynchronous status, progress, and distributed behavior. |
| Alias | P2 | `CreateAlias`, `DropAlias`, `AlterAlias`, `DescribeAlias`, `ListAliases` | Planned, including cache invalidation and authorization/database boundaries. |
| Index | P6 | `CreateIndex`, `AlterIndex`, `DescribeIndex`, `GetIndexStatistics`, `DropIndex` | Create/describe/drop exists for a few indexes; add properties, status, and the parameter matrix. |
| Legacy index status | P6/compatibility | `GetIndexState`, `GetIndexBuildProgress` | Proto marks these deprecated. Prefer a unified DescribeIndex mapping with semantic evidence rather than new obsolete SQL interfaces. |
| Mutation | P3 | `Insert`, `Delete`, `Upsert` | Basic paths exist; add typed data, partitions, partial updates, results, and all options. |
| Read/search | P3/P4/P5 | `Query`, `Search`, `HybridSearch` | Query/search subset exists; advanced parameters, expressions, iteration, functions, and hybrid search remain planned. |
| Flush | P2 | `Flush`, `FlushAll`, `GetFlushState`, `GetFlushAllState` | Collection flush exists; add asynchronous behavior, global scope, and status. |
| Compaction | P7 | `ManualCompaction`, `GetCompactionState`, `GetCompactionStateWithPlans` | Planned; return native task IDs and verify state/plans. |
| Import | P7 | `Import`, `GetImportState`, `ListImportTasks` | Planned. This group is not the complete REST v2 import surface; audit versioned REST routes separately. |
| User/role | P7 | `CreateCredential`, `UpdateCredential`, `DeleteCredential`, `ListCredUsers`, `CreateRole`, `AlterRole`, `DropRole`, `OperateUserRole`, `SelectRole`, `SelectUser` | Planned; SQL-principal to Milvus-principal mapping, role descriptions, and revocation for existing sessions. |
| Privilege/RBAC | P7 | `OperatePrivilege`, `OperatePrivilegeV2`, `SelectGrant`, `BackupRBAC`, `RestoreRBAC`, `CreatePrivilegeGroup`, `DropPrivilegeGroup`, `ListPrivilegeGroups`, `OperatePrivilegeGroup` | Planned; database/collection scopes, privilege groups, and backup/restore. |
| Resource group | P7 | `CreateResourceGroup`, `DropResourceGroup`, `UpdateResourceGroups`, `TransferNode`, `TransferReplica`, `ListResourceGroups`, `DescribeResourceGroup`, `GetReplicas` | Planned; validate in a dedicated distributed environment. |
| Public diagnostics and restricted administration | P7 | `GetPersistentSegmentInfo`, `GetQuerySegmentInfo`, `GetMetrics`, `GetComponentStates`, `LoadBalance` | Planned, subject to verifying each public administration contract; distinguish authorization, reads, and mutations. |
| Version/health | P0/P7 | `GetVersion`, `CheckHealth` | Capability/status exposure is planned. A SQL SELECT 1 readiness probe does not replace upstream health checks. |
| Analyzer | P5 | `RunAnalyzer` | Planned; define parameter and token/offset result contracts. |
| Connection internals | Adapter-internal | `Connect` | Used by the SDK/connection adapter; no separate user-facing SQL command is required. |
| Replication administration | P7; contract verification pending | `UpdateReplicateConfiguration`, `GetReplicateConfiguration`, `GetReplicateInfo` | First verify public availability and deployment applicability, then choose restricted procedures or native-tool integration. Do not claim complete administration coverage while unresolved. |
| Replication/coordination/recovery | Native-tool boundary | `ReplicateMessage`, `CreateReplicateStream`, `DumpMessages`, `AllocTimestamp`, `RegisterLink` | Internal streams, timestamp/link coordination, and WAL recovery are not ordinary SQL interfaces. Retain tool integration and runtime compatibility validation. |
| Internal debugging | Not exposed | `Dummy`, `ListIndexedSegment`, `DescribeSegmentIndexData` | Debugging/internal index data APIs are not exposed through arbitrary SQL access. The P0 audit must still record pinned-version evidence. |
| Deprecated distance RPC | Unavailable upstream | `CalcDistance` | The pinned Proxy returns CalcDistance deprecated directly; do not promise support for this RPC. |
| File-resource placeholder RPCs | Unimplemented upstream | `AddFileResource`, `RemoveFileResource`, `ListFileResources` | The pinned Proxy returns not implemented; these are not released user capabilities. |
| Tag/row-policy placeholder RPCs | Unimplemented upstream | `AddUserTags`, `DeleteUserTags`, `GetUserTags`, `ListUsersWithTag`, `CreateRowPolicy`, `DropRowPolicy`, `ListRowPolicies` | The pinned service embeds UnimplementedMilvusServiceServer without overriding these methods. Proto declarations do not justify promises of user tags or row-level policies. |

`ProxyService.RegisterLink` belongs to a separate service in the same proto file. It is excluded from the MilvusService count above and is not exposed to SQL users. An RPC used internally by the SDK does not mean its user-facing capability is covered by SQL.

## Capabilities Beyond the RPC Table

| Dimension | Required P0 inventory | Delivery stage |
| --- | --- | --- |
| Types/schema | Every public type, element/nested type, nullable/default behavior, primary key, dynamic/partition/clustering keys, field properties, and permitted alterations | P1/P2 |
| Expressions | Arithmetic/comparison/logic, JSON/ARRAY, NULL/absence, text, GIS, time, templates, and type combinations | P3/P5 |
| Request parameters | Collection/database/partition, consistency, timestamps, timeouts, and all public search/load/write options; server defaults and limits | P0–P7 |
| SDK composite operations | Get-by-ID, query/search iterators, task waits, hybrid search/reranking; do not omit operations because they lack a dedicated RPC | P3–P5 |
| Indexes/metrics | Extract enums and constraints from pinned registries/validators, then reconcile with actual CPU/GPU builds and official index documentation | P6 |
| Functions/providers | BM25, TextEmbedding, and Rerank types/parameters/inputs/outputs/environment restrictions; analyzers and highlighting | P5 |
| REST-only/new entry points | Especially import v2 job create/list/progress/describe; verify differences from legacy Import | P7 |
| Internal architecture | Streaming/WAL/storage deployment compatibility and failure recovery; native operations-tool integration | P7/P8 |

Type caveat: the `Text` enum in `schema.proto` does not establish TEXT column support. The 2.6.23 release notes explicitly describe a fix rejecting the unsupported Text type. Full-text capabilities use supported VARCHAR/analyzer/function facilities. Legacy enums such as `String` and internal `ArrayOfStruct` must also be validated against the server rather than exposed directly as SQL types. [Release notes](https://milvus.io/docs/v2.6.x/release_notes.md)

## Known Patch-Version Gates

These are verified representative gates, **not a complete minimum-version table**. P0 must populate the full registry and validate each target server + SDK/adapter + deployment combination.

| Capability | Upstream version gate | Implementation requirement |
| --- | --- | --- |
| Partial upsert merge | 2.6.2+ | Native partial_update; separately test AutoID, omitted fields, and dynamic/JSON semantics |
| Array of Structs | 2.6.4+ | Register schema, nested types, and search options separately; do not assume later extensions were all available in 2.6.4 |
| Text highlighter | 2.6.8+ | Test fields/tags/result structures; explicitly reject on 2.6.2 |
| ARRAY_APPEND/ARRAY_REMOVE | 2.6.17+ | Native field_ops and element/capacity constraints |
| Nullable vectors and Struct element-level search | Introduced in 2.6.18 | A schema-encoding upgrade does not make older servers support them; test field/filter restrictions separately |

Sources: [release history](https://milvus.io/docs/v2.6.x/release_notes.md), [Upsert](https://milvus.io/docs/v2.6.x/upsert-entities.md), and [Highlighter](https://milvus.io/docs/text-highlighter.md). Living documentation explains behavior; pinned tags and real execution establish acceptance. Features introduced after 2.6.23 do not automatically enter this baseline.

## Closing Exclusions and Unknowns

- Unimplemented/deprecated upstream: cite responses or service entry-point evidence at the pinned tag. Reevaluate if a future target restores the capability.
- Internal/tool boundaries: document purpose, why ordinary SQL does not expose the interface, the official tool path, and required proxy compatibility tests. Implementation difficulty alone cannot turn a public administration capability into an internal exclusion.
- Unverified contracts: supply official public documentation, server implementation, deployment prerequisites, and a minimal execution test. Then assign a delivery stage or an evidenced boundary. Do not claim full coverage while these remain unresolved.
- Break each implemented capability down further by parameter and result contracts. This RPC table prevents omissions; it is not the denominator for support percentages or completion rates.
