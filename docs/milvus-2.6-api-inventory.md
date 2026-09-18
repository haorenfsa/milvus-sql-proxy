# Milvus 2.6.23 API 盘点

更新日期：2026-09-18。配套 [Roadmap](../ROADMAP.md)。

这是一份**范围与缺口清单**，不是实现完成清单。按固定版本 `MilvusService` 的 117 个 RPC 声明逐项分组，名称无遗漏/重复。范围分类依据公开用途、服务实现及项目边界；proto-only、deprecated、内部接口不能计作待实现的普通 SQL 能力。

源码依据：

- [milvus.proto v2.6.23](https://github.com/milvus-io/milvus-proto/blob/v2.6.23/proto/milvus.proto)：RPC 请求/响应全集。
- [schema.proto v2.6.23](https://github.com/milvus-io/milvus-proto/blob/v2.6.23/proto/schema.proto)：类型、函数、字段及搜索结果结构。
- [Proxy impl v2.6.23](https://github.com/milvus-io/milvus/blob/v2.6.23/internal/proxy/impl.go)：实际实现、废弃及未实现返回。
- [Proxy service v2.6.23](https://github.com/milvus-io/milvus/blob/v2.6.23/internal/distributed/proxy/service.go)：暴露入口和 Unimplemented server。
- [REST v2 handler v2.6.23](https://github.com/milvus-io/milvus/blob/v2.6.23/internal/distributed/proxy/httpserver/handler_v2.go)：REST 补充入口，P0 需完整提取路由与请求字段。

## RPC 到阶段的映射

| 能力族 | 阶段/边界 | RPC | 当前情况与待办 |
| --- | --- | --- | --- |
| Database | P2 | `ListDatabases`, `CreateDatabase`, `DropDatabase`, `DescribeDatabase`, `AlterDatabase` | 已有 list/create/drop 子集；补 describe/alter/属性与跨库行为。 |
| Collection | P1/P2 | `CreateCollection`, `DropCollection`, `HasCollection`, `DescribeCollection`, `BatchDescribeCollection`, `ShowCollections`, `GetCollectionStatistics`, `AlterCollection`, `AlterCollectionField`, `AddCollectionField`, `RenameCollection`, `TruncateCollection` | 已有基础 create/drop/list/describe；其余操作及全部 schema/属性参数待补。 |
| Function DDL | P5 | `AddCollectionFunction`, `AlterCollectionFunction`, `DropCollectionFunction` | 待支持；创建时的 function schema 也纳入 P1/P5。 |
| Partition | P2/P3 | `CreatePartition`, `DropPartition`, `HasPartition`, `ShowPartitions`, `GetPartitionStatistics` | 已有 create/drop/list；补统计、存在性、路由与 key 分区语义。 |
| Load | P2 | `LoadCollection`, `ReleaseCollection`, `LoadPartitions`, `ReleasePartitions`, `GetLoadingProgress`, `GetLoadState` | 已有同步 load/release 子集；补参数、异步状态、进度和分布式行为。 |
| Alias | P2 | `CreateAlias`, `DropAlias`, `AlterAlias`, `DescribeAlias`, `ListAliases` | 待支持；涵盖缓存失效和权限/数据库边界。 |
| Index | P6 | `CreateIndex`, `AlterIndex`, `DescribeIndex`, `GetIndexStatistics`, `DropIndex` | 已有少量索引 create/describe/drop；补属性、状态及参数矩阵。 |
| 旧 index 状态接口 | P6/兼容 | `GetIndexState`, `GetIndexBuildProgress` | proto 标记 deprecated；优先统一到 DescribeIndex，保留语义映射证据，不另造过时 SQL。 |
| Mutation | P3 | `Insert`, `Delete`, `Upsert` | 已有基础路径；补 typed data、分区、partial update、结果和全部选项。 |
| Read/search | P3/P4/P5 | `Query`, `Search`, `HybridSearch` | 已有 query/search 子集；高级参数、表达式、迭代、函数、hybrid 待支持。 |
| Flush | P2 | `Flush`, `FlushAll`, `GetFlushState`, `GetFlushAllState` | 已有 collection flush；补异步、全局范围与状态。 |
| Compaction | P7 | `ManualCompaction`, `GetCompactionState`, `GetCompactionStateWithPlans` | 待支持；返回原生任务 ID，验证状态与 plans。 |
| Import | P7 | `Import`, `GetImportState`, `ListImportTasks` | 待支持；此组不代表 REST v2 import 全集，另盘点版本化 REST 路由。 |
| User/role | P7 | `CreateCredential`, `UpdateCredential`, `DeleteCredential`, `ListCredUsers`, `CreateRole`, `AlterRole`, `DropRole`, `OperateUserRole`, `SelectRole`, `SelectUser` | 待支持；SQL principal 到 Milvus principal 映射、角色描述及会话撤权。 |
| Privilege/RBAC | P7 | `OperatePrivilege`, `OperatePrivilegeV2`, `SelectGrant`, `BackupRBAC`, `RestoreRBAC`, `CreatePrivilegeGroup`, `DropPrivilegeGroup`, `ListPrivilegeGroups`, `OperatePrivilegeGroup` | 待支持；数据库/collection 范围、权限组和备份恢复。 |
| Resource group | P7 | `CreateResourceGroup`, `DropResourceGroup`, `UpdateResourceGroups`, `TransferNode`, `TransferReplica`, `ListResourceGroups`, `DescribeResourceGroup`, `GetReplicas` | 待支持；专用分布式环境验证。 |
| 公开诊断与受限管理 | P7 | `GetPersistentSegmentInfo`, `GetQuerySegmentInfo`, `GetMetrics`, `GetComponentStates`, `LoadBalance` | 待支持/逐项确认公开管理契约；管理鉴权、只读与 mutation 分开。 |
| Version/health | P0/P7 | `GetVersion`, `CheckHealth` | 待暴露能力/状态；SQL SELECT 1 就绪探测不能代替上游健康检查。 |
| Analyzer | P5 | `RunAnalyzer` | 待支持；参数、token、offset 等结果契约。 |
| 连接内部机制 | adapter 内部 | `Connect` | 由 SDK/连接 adapter 使用；不需要一条对用户开放的 SQL。 |
| 复制管理 | P7 待核契约 | `UpdateReplicateConfiguration`, `GetReplicateConfiguration`, `GetReplicateInfo` | 先验证目标版本公开/部署适用性，再选择受限管理过程或原生工具集成；关闭此项前不得宣称管理能力全覆盖。 |
| 复制/协调/救援 | 原生工具边界 | `ReplicateMessage`, `CreateReplicateStream`, `DumpMessages`, `AllocTimestamp`, `RegisterLink` | 内部数据流、时间戳/链路和 WAL 救援，不作为普通 SQL 入口；保留工具集成与运行兼容验证。 |
| 内部调试 | 不暴露 | `Dummy`, `ListIndexedSegment`, `DescribeSegmentIndexData` | 调试/索引内部数据接口，不以通用 SQL 任意访问；P0 审计仍需记录固定版本依据。 |
| 废弃距离 RPC | 上游不可用 | `CalcDistance` | 固定版本 Proxy 直接返回 CalcDistance deprecated；不承诺实现这条 RPC。 |
| 文件资源占位 RPC | 上游未实现 | `AddFileResource`, `RemoveFileResource`, `ListFileResources` | 固定版本 Proxy 返回 not implemented；不作为已发布用户能力。 |
| 标签/行策略占位 RPC | 上游未实现 | `AddUserTags`, `DeleteUserTags`, `GetUserTags`, `ListUsersWithTag`, `CreateRowPolicy`, `DropRowPolicy`, `ListRowPolicies` | 固定版本服务嵌入 UnimplementedMilvusServiceServer，未提供这些方法的 override；不能据 proto 承诺用户标签或行级策略。 |

`ProxyService.RegisterLink` 是同一 proto 文件中的另一个服务，不计入上面的 MilvusService 数量，也不对 SQL 用户开放。SDK 内部调用 RPC 不代表用户 SQL 已覆盖该能力。

## RPC 表之外必须登记的能力

| 维度 | P0 清单要求 | 交付阶段 |
| --- | --- | --- |
| 类型/schema | 每个公开类型、元素/嵌套类型、nullable/default、PK、动态/partition/clustering key、字段属性及可修改范围 | P1/P2 |
| 表达式 | 算术/比较/逻辑、JSON/ARRAY、NULL/缺失、文本、GIS、时间、模板与各类型组合 | P3/P5 |
| 请求参数 | collection/database/partition、consistency、时间戳、超时、搜索/加载/写入所有公开选项；服务端默认值与限制 | P0–P7 |
| SDK 组合操作 | Get-by-ID、query/search iterator、等待任务、hybrid/rerank 等；不能因无独立 RPC 而漏掉 | P3–P5 |
| 索引/metric | 从固定版本 registry/validator 提取枚举和参数约束，再与 CPU/GPU 实际构建和官方索引文档交叉核对 | P6 |
| 函数与 provider | BM25、TextEmbedding、Rerank 的类型/参数/输入输出/环境约束，Analyzer 与 highlighter | P5 |
| REST-only/新入口 | 特别是 import v2 job create/list/progress/describe；确认与旧 Import 的差异 | P7 |
| 内部架构 | streaming/WAL/storage 的部署兼容、失败恢复；原生运维工具集成说明 | P7/P8 |

类型注意：`schema.proto` 的 `Text` 枚举不证明支持 TEXT 列；2.6.23 发布说明明确修复了错误接受不支持 Text 类型的问题。全文能力使用支持的 VARCHAR/analyzer/function。`String` 等旧枚举、内部 `ArrayOfStruct` 也必须经 server 校验，不能直接作为 SQL 类型开放。[发布说明](https://milvus.io/docs/v2.6.x/release_notes.md)

## 已知补丁门槛

以下为已核实的代表性门槛，**不是全部功能的最小版本表**。P0 需要填充完整 registry，按目标 server + SDK/adapter + 部署条件实际验证。

| 能力 | 上游标注门槛 | 实施要求 |
| --- | --- | --- |
| partial upsert merge | 2.6.2+ | 原生 partial_update；AutoID、缺失字段、动态/JSON 语义分别测试 |
| Array of Structs | 2.6.4+ | schema、嵌套类型和 search 选项分别登记，不认为所有后续扩展从 2.6.4 就可用 |
| text highlighter | 2.6.8+ | 字段/标签/结果结构测试；2.6.2 明确拒绝 |
| ARRAY_APPEND/ARRAY_REMOVE | 2.6.17+ | 原生 field_ops 与元素/容量约束测试 |
| nullable vectors、Struct 元素级搜索 | 2.6.18 新增 | 不能仅升级 schema 编码就宣称旧版本支持；字段/过滤限制单独测试 |

来源：[版本记录](https://milvus.io/docs/v2.6.x/release_notes.md)、[Upsert](https://milvus.io/docs/v2.6.x/upsert-entities.md)、[Highlighter](https://milvus.io/docs/text-highlighter.md)。流动文档只作解释，固定 tag 和真实行为用于验收；2.6.23 之后的功能不自动进入本基线。

## 排除与未知项的关闭规则

- 上游未实现/废弃：引用固定 tag 中返回值或服务入口证据；以后目标版本恢复时重新评估。
- 内部/工具边界：写明用途、为什么不开放普通 SQL、已有官方工具路径，以及代理所需兼容性测试。公开管理能力不能仅因实现复杂就改成“内部”。
- 待核契约：必须补官方公开文档、服务实现、部署前提和最小调用验证，随后分配 P 阶段或有依据的边界；未关闭时不得宣布全覆盖。
- 每项已实现能力继续按参数与结果合同拆分；本 RPC 表只防止遗漏，不作为支持百分比或完成率的分母。
