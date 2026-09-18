# Milvus 2.6 全功能支持 Roadmap

更新日期：2026-09-18。当前实现基线：`077de63`（PR #2）。

目标：通过 MySQL 和 PostgreSQL 两种协议，完整提供 **Milvus 2.6.23 已实现、面向用户的数据与管理能力**。两个协议使用同一个能力模型、执行计划和权限语义；`mysql`、`postgres`、`both` 三种启动模式持续通过 E2E。

这是实现计划，文中的新增 SQL、过程名及阶段均为**待实现草案**，不能作为当前支持说明。当前可用语法以 [Readme.md](Readme.md) 为准。

## 1. 版本与范围

- **完整支持目标固定为 2.6.23**；这是本次核对的官方 2.6 发布基线。后续 2.6 补丁通过独立兼容性 PR 更新目标，不能悄悄扩大支持声明。[发布说明](https://milvus.io/docs/v2.6.x/release_notes.md)
- **2.6.2 保留已有功能回归**，不承诺它拥有后续补丁新增的能力。每项功能记录最低已验证 server/SDK 版本、部署条件和测试证据。
- 覆盖数据模型、DDL、DML、查询/搜索、索引、函数、导入、权限、资源管理和公开运维 API；每个功能的请求参数、结果类型、状态查询和失败行为都属于范围。
- GPU、分布式资源组、外部模型服务等能力属于目标，需要专用测试环境；缺少环境时标记“未验证”，不能算完成。
- JOIN、跨集合事务、外键、通用 SQL 聚合/窗口函数、任意表达式 UPDATE、完整 MySQL/PostgreSQL 系统目录不属于 Milvus 原生能力。代理不以隐式全量拉取、读改写模拟这些语义。
- Streaming Node、Woodpecker、Storage V2、JSON shredding 等服务端内部实现通过兼容性测试验证；安装、扩缩容、备份/CDC 工具和集群配置有各自运维边界。它们不等同于新增 SQL 运算符。
- proto 中存在不等于服务端可用：废弃、未实现、内部复制/救援接口逐项列在 [API 盘点](docs/milvus-2.6-api-inventory.md)，附依据和处理方式，不能无声遗漏或算作完成。

## 2. 当前基础与缺口

| 领域 | 当前已实现 | 距离完整支持的主要缺口 |
| --- | --- | --- |
| 协议 | MySQL text/prepared；PG simple/extended；双端口；TLS 配置 | 中立结果模型、更多类型编码、取消、流式结果、稳定错误映射 |
| SDK | `milvus-sdk-go/v2`、proto 均为 2.3.3 | 升级现代 `milvus/client/v2`，补齐 SDK 缺口和能力门控 |
| Schema | 基础标量、JSON、float32 vector、主键/AutoID | nullable/default、动态字段、ARRAY、其他向量、结构数组、GIS、时间类型、schema 演进 |
| 管理 | DB/collection CRUD 子集、手工分区、load/release/flush | 属性、别名、统计、状态、异步任务、分区键、资源组、compaction、RBAC |
| 数据 | insert、全行 upsert、带过滤 delete | 分区路由、partial upsert、自动 ID 结果、完整 mutation 计数、bulk import |
| 查询 | 投影、基础过滤、count、分页、单条 dense ANN | 全表达式、batch NQ、稀疏/BM25、hybrid、rerank、范围/分组/迭代搜索 |
| 索引 | FLAT/HNSW/IVF_FLAT/AUTOINDEX/INVERTED | 其余索引、参数矩阵、状态/进度、属性变更、JSON/文本/空间索引 |
| 验证 | 2.6.2，实际二进制 × 三种模式；覆盖率门槛 90% | 2.6.23 全能力验证、SDK 对照、类型逐值断言、分布式/GPU/模型测试 |

依据：[go.mod](go.mod)、[执行层](pkg/conn.go)、[SQL 命令](pkg/commands.go)、[生命周期测试](pkg/integration_test.go)、[E2E](.github/workflows/e2e.yml)。这些基础能力仍需扩充参数和结果断言，不能整体标为“全功能完成”。

## 3. 设计约定

1. **一份 AST/计划，两种协议。** 将共享执行层当前使用的 MySQL result 类型替换为协议中立的列元数据、值、mutation 结果和结构化错误；协议层只负责绑定与编码。
2. **结构化解析。** 扩展语法进入明确 AST，逐步替换 lifecycle/index 的正则分派。参数绑定携带类型，不拼接不可信 filter、JSON 或管理参数；复合类型有大小/维度/深度限制。
3. **SQL 加显式 Milvus 扩展。** 常规 CRUD/DDL 使用 SQL；复杂搜索、异步任务及管理能力使用具名 `CALL milvus_*` 扩展。为每个过程固定参数 schema、结果列、权限、取消和版本要求，禁止提供任意 RPC/管理透传后门。
4. **忠实保留语义。** 区分 SQL NULL、缺失动态键、JSON null；保留向量精度、距离方向、NQ 分组和排名；不把 ANN group-by 解释成关系聚合，不把异步提交成功解释成任务完成。
5. **版本和环境共同决定能力。** `SHOW MILVUS CAPABILITIES`（草案）显示 server/SDK 版本、能力、限制、部署前提和验证状态。版本未知时不能猜测支持；必要时采用只读探测及显式配置。
6. **权限贯穿执行链。** 每个会话有独立上游身份/数据库上下文；跨库、别名、导入、管理过程也必须鉴权。现有单个共享上游账号模式明确标为 service-account 模式，不能声称已有用户级 RBAC。
7. **受控 SDK 补缺。** 首选固定版本官方 Go SDK；缺失能力封装到窄接口的 gRPC/REST v2 adapter，复用身份、TLS、超时和错误处理。不能因为 SDK 缺失就把 Milvus 能力从范围中删除。

## 4. 实施顺序与验收

阶段按依赖推进，不承诺未经估算的日期。P0 建立契约后，P1–P3 构成基础数据闭环；P4–P7 补齐高级与管理能力；P8 完成全量验收。P7 的身份设计从 P0 开始，管理功能发布必须受它约束。

### P0 — SDK、能力清单与双协议基础

- [ ] 升级到固定的现代 Go SDK/proto，逐项迁移 session client、schema、index、mutation 和异步等待行为；保留 2.6.2 已有功能回归。
- [ ] 建立机器可读 feature registry：`id / operation / parameters / min_server / sdk_or_adapter / deployment / mysql / postgres / tests / status / exclusion_reason`。
- [ ] 将 API 盘点扩展到参数、schema 枚举、表达式、索引及 SDK 组合操作；每个条目有阶段和实现/测试链接。
- [ ] 建立协议中立的值/结果/错误模型；定义两端的 SQLSTATE/错误码、参数类型、64 位整数、NULL、二进制和复杂类型编码。
- [ ] 定义取消与超时、mutation 重试边界、幂等性和任务状态契约；超时不自动重放可能已提交的写操作。
- [ ] 将 E2E 增加固定 2.6.23 目标，保留 2.6.2 回归；先迁移再增加语法，避免 SDK 升级与全部新特性混成一个 PR。

**验收：** 原有测试与三模式 E2E 在两个固定版本上通过；已有 SQL 行为不回退；未知能力有明确错误，不能静默忽略参数。

### P1 — 完整数据类型与 Schema

- [ ] 基础类型边界：整数范围、浮点精度、VARCHAR 长度、JSON、INT64/VARCHAR 主键、AutoID 和 `allow_insert_auto_id` 等适用属性。
- [ ] nullable/default、字段省略、显式 NULL 与默认值；按 server 对类型/主键/分区键的限制校验，不能对所有类型一律开放默认值。
- [ ] 动态字段开关、读写、投影、嵌套路径和 schema 描述；支持服务端允许的开启/演进操作。
- [ ] ARRAY 的元素类型/max_capacity；FloatVector、Float16Vector、BFloat16Vector、Int8Vector、BinaryVector、SparseFloatVector 的构造、绑定与返回。
- [ ] Array of Structs、其中的向量子字段/多向量列表、嵌套结果与元素级检索所需 schema。不能把内部 `ArrayOfStruct` 枚举直接当用户顶层任意 STRUCT。
- [ ] GEOMETRY/WKT、TIMESTAMPTZ/时区、相应 NULL 与过滤/索引支持。
- [ ] 字段描述、analyzer/function output、partition key、clustering key；新增字段及可修改字段属性，保留不允许的 schema 修改错误。

**验收：** 每个类型在 MySQL text/prepared 和 PG text/binary 结果中逐值往返；覆盖空数组、稀疏空向量、NULL、越界、坏 WKT、时区/DST、维度错误；Describe 不触发写入。类型/默认值限制以固定版本 schema 与执行结果为准。[类型定义](https://github.com/milvus-io/milvus-proto/blob/v2.6.23/proto/schema.proto)

### P2 — DB、Collection、Partition 与生命周期

- [ ] Database list/describe/create/alter/drop；属性设置/删除和默认值继承；跨库 collection 访问与移动/rename 的适用限制。
- [ ] Collection has/list/describe/batch describe/statistics/create/rename/drop/truncate；shards、consistency、TTL、mmap、时区等已支持属性与参数。
- [ ] Alias create/alter/drop/list/describe；变更后失效旧 schema/index 缓存，跨库解析不能串库。
- [ ] 手工 partition has/list/create/drop/statistics/load/release；partition key、自动分区数、partition-key isolation 和 clustering key。
- [ ] Load/release 的 replica、resource groups、指定字段、skip dynamic field 等适用选项；load state/progress。
- [ ] Flush/flush-all、异步状态和等待；明确“数据已提交”“已持久化”“已加载可查”的不同状态。
- [ ] 需要的 `IF EXISTS / IF NOT EXISTS` 基于真实存在性与并发错误处理，不吞权限和网络错误。

**验收：** 对照 SDK 检查所有属性与统计；别名重指向、跨会话/跨库隔离、异步失败、reload、分区加载后可见性、truncate 保留 schema/index 等行为正确。

### P3 — DML 与完整过滤/查询

- [ ] INSERT/full UPSERT/partial UPSERT：命名列、分区选择、AutoID 语义、返回主键/时间戳/计数；没有上游精确计数时不能伪造。
- [ ] partial upsert 的字段省略、动态键 merge、JSON 整列替换、ARRAY_APPEND/ARRAY_REMOVE；以原生 Upsert 请求实现，不做代理端隐式读改写。[Upsert 语义](https://milvus.io/docs/v2.6.x/upsert-entities.md)
- [ ] DELETE 按主键/过滤表达式及分区；显式全量删除接口与防误操作语法；不能将当前拒绝裸 DELETE 的行为悄悄改为全量删除。
- [ ] Get-by-ID、query、count、字段/动态键/嵌套投影、别名和迭代读取；返回字段顺序、空结果元数据与计数语义。
- [ ] 过滤比较/逻辑/算术、IN、LIKE、NULL、JSON/ARRAY contains/contains_all/contains_any、长度/下标/路径、存在性、文本和 GIS/时间表达式；逐运算符登记版本/类型约束。
- [ ] 参数化过滤模板、unicode/转义/注入边界；处理缺失键、JSON null、类型不匹配的三值逻辑。
- [ ] Strong/Bounded/Session/Eventual 及服务端公开的 consistency/时间戳选项；会话级与请求级覆盖、时间戳单位与适用限制。
- [ ] Query iterator 生命周期、fetch/close/断连清理、背压和有界内存；不通过不断增加 offset 冒充迭代器。

**验收：** SDK 差分测试逐字段/计数核对；覆盖过滤真值表、写后读、分区隔离、多个客户端、超过单次窗口的数据遍历；partial upsert 的非事务并发语义写入文档。

### P4 — 向量搜索全能力

- [ ] 所有向量类型的 single/batch NQ；结果带 query index、rank、PK、distance/score，保留每个 query 的边界。
- [ ] 搜索字段/分区、topK/offset、索引专用搜索参数、metric、round_decimal、ignore_growing、输出字段及动态字段。
- [ ] 标准/迭代过滤、range search、search iterator；控制 radius/range_filter 的不同 metric 方向和边界。
- [ ] Grouping search：group_by_field、group_size、strict_group_size 等固定版本支持项；不是通用 SQL GROUP BY。
- [ ] 多路 HybridSearch、每路 ANN 请求/过滤/候选数，RRF 和 WeightedRanker、最终 limit/offset 与 score。[融合说明](https://milvus.io/docs/v2.6.x/reranking.md)
- [ ] Array-of-vector/EmbeddingList、结构数组元素搜索、服务端支持的聚合相似度与结果定位；不把多向量字段单独检索当 hybrid 已完成。
- [ ] 按主键搜索等固定版本公开搜索输入，以及高级检索选项，完成 SDK/请求参数清单后逐项实现；不凭 proto 字段直接开放。

**验收：** FLAT 精确 oracle + 固定随机种子；ANN 使用明确 recall 门槛，距离浮点容差、并列排名规则、分组数及 NQ 隔离独立断言。Hybrid 分数/顺序对照官方 SDK，而非只检查非空结果。

### P5 — 全文、Analyzer、函数与重排

- [ ] 文本字段 analyzer 配置、tokenizer/filter、多语言/多 analyzer（按版本）、TEXT_MATCH、PHRASE_MATCH；RunAnalyzer 和 token 输出选项。
- [ ] BM25 function/schema、稀疏索引、原始文本搜索、稀疏/稠密混合检索、函数输出字段读写限制。
- [ ] Collection function 的 create/add/alter/drop/describe；TextEmbedding 的服务端已支持 provider/config，输入输出字段映射和错误处理。
- [ ] Function rerank：目标版本实际支持的模型、decay、boost 等类型/参数；与 hybrid 的 RRF/weighted 分开登记。
- [ ] Text highlighter 的来源、字段、标签、片段和结果结构；处理空值、unicode 和动态文本。
- [ ] MinHash/去重链路：2.6 已支持的预计算签名 + MINHASH_LSH/MHJACCARD；**不把后续版本的服务端 MinHash function 移植承诺到 2.6**。

**验收：** 固定语料验证 token、BM25 排序和高亮；模型 adapter 使用契约测试，并在声明支持的真实 provider 上做可重复验证。日志不得泄露 provider 凭据；未跑真实 provider 的组合标为未验证。

### P6 — 索引与性能选项

- [ ] 建立“字段类型 × index × metric × build/search 参数 × CPU/GPU × 最低版本”清单，不以字符串透传替代支持。
- [ ] dense：补齐 IVF_SQ8、IVF_PQ、SCANN、HNSW 的量化变体/精排选项、IVF_RABITQ 等目标版本支持项，以及 DISKANN、AISAQ；保留现有索引。
- [ ] binary/sparse：BIN_FLAT、BIN_IVF_FLAT、MINHASH_LSH、SPARSE_INVERTED_INDEX 与适用算法；旧 SPARSE_WAND 按上游弃用策略兼容/报错。
- [ ] GPU：GPU_CAGRA、GPU_IVF_FLAT、GPU_IVF_PQ、GPU_BRUTE_FORCE 等目标构建实际注册项；声明硬件/镜像/内存约束。
- [ ] scalar：INVERTED、BITMAP、STL_SORT、TRIE、AUTOINDEX 等适用类型；JSON path/flat/cast 参数、ARRAY、NGRAM、文本匹配、GEOMETRY RTREE。
- [ ] Index create/list/describe/drop/alter、statistics/state/progress、异步构建失败；mmap、缓存/预热等公开选项与实际生效边界。

**验收：** 从固定版本 index registry/validator 核对枚举；每个合法组合实际建索引、load、search/query、release、修改/删除并校验结果。非法组合明确失败；没有 GPU/磁盘环境的索引不能标完成。参考 [索引与 metric](https://milvus.io/docs/v2.6.x/index-vector-fields.md)（该概览已弃用，最终清单以固定版本实现与各索引文档为准）。

### P7 — 导入、权限、资源与运维管理

- [ ] Bulk import：原生 import job 创建/list/progress/state、文件格式/存储路径/选项、失败原因和幂等边界；REST v2 能力独立核对，不能只看旧 gRPC Import。
- [ ] CSV/JSON/Parquet/NumPy 等目标版本实际支持格式逐项测试；数据库、分区、AutoID、nullable、所有支持类型与导入后索引/加载闭环。
- [ ] User/password/role/member、grant/revoke/list、Privilege V2、privilege group、role description、RBAC backup/restore；定义 SQL 用户与 Milvus principal 的映射。
- [ ] 管理默认关闭，显式启用且使用最小上游权限；密码更新、权限撤销、会话存活与跨租户隔离均测试。共享 root 后端不能冒充用户级授权。
- [ ] Resource group create/update/describe/list/drop、node/replica transfer、replica 配置/查询；query/streaming 资源组支持范围按目标部署核对。
- [ ] Manual/clustering compaction、state/plans、segment/replica/statistics；公开 load balance 管理动作独立权限与审计。
- [ ] Version/health、公开 metrics、代理请求日志/trace/延迟/错误/在途数；敏感 SQL 参数脱敏，取消和超时可关联到上游。
- [ ] CDC/replication 公共配置与状态接口先确认支持契约，必要时提供受限管理过程；复制数据流、WAL dump、内部协调接口交给原生工具并记录集成边界。

**验收：** 真实启用鉴权的独立 Milvus 验证拒绝与允许；对象存储导入失败/恢复与行值校验；分布式多 QueryNode 验证资源组和副本迁移。所有 mutation 只操作测试创建的资源并完成清理。

### P8 — 兼容性与发布验收

- [ ] MySQL text/binary/prepared、PG simple/extended/text/binary 的能力矩阵全部通过；NULL、空结果、复杂列元数据一致。
- [ ] PostgreSQL CancelRequest 和 MySQL 查询取消路径、断连、statement/portal 关闭、超时后连接恢复、优雅退出；资源有界释放。
- [ ] 真实 `mysql`/`psql` 和 Go/Python/JDBC 常用驱动 smoke；最小元数据兼容集单独文档化，不承诺所有 ORM。
- [ ] 大 NQ、大批写入、大结果、并发会话、慢客户端、上游重启/连接失败、权限撤销与任务超时；规定吞吐、p95/p99、内存基线后测量代理开销。
- [ ] 完成 capability registry 的参数与异常分支审计；公开状态、SQL/API 对照、最低版本和测试链接。

**验收：** 所有范围内条目均有双协议真实 E2E 证据；没有 P0/P1 缺陷；排除项有上游依据。专用环境未验证项目意味着“部分支持”，不能发布“Milvus 2.6 全功能支持”的声明。

## 5. SQL 接口草案

以下仅确定方向；实现前在对应 PR 固定语法、类型及错误契约，并增加 parser/protocol 测试。`CALL` 名称不是上游 Milvus SQL 标准。

```sql
-- P1/P2：扩展 DDL 与属性；WITH 的键和值按目标能力注册校验
CREATE TABLE docs (
    id BIGINT PRIMARY KEY,
    title VARCHAR(512),
    rating DOUBLE DEFAULT 0,
    tags ARRAY<VARCHAR(64)>,
    embedding VECTOR(3)
) WITH (enable_dynamic_field=true);
ALTER TABLE docs ADD COLUMN category VARCHAR(64) NULL;
SHOW MILVUS CAPABILITIES;

-- P3：显式 partial update，不许隐式改成关系型 UPDATE
UPSERT INTO docs (id, title) VALUES (1, 'new title')
    WITH (partial_update=true);

-- P4/P5：复杂请求通过强类型、具名过程；JSON 仅为绑定载体
CALL milvus_search('docs', '{"anns_field":"embedding","data":[[1,0,0]],"limit":10}');
CALL milvus_hybrid_search('docs', '<validated requests and ranker JSON>');
CALL milvus_run_analyzer('<validated analyzer and text JSON>');

-- P7：长任务返回原生 job ID，可独立查询/等待
CALL milvus_import('docs', '<validated import request JSON>');
CALL milvus_import_status('<job id>');
CALL milvus_compact('docs', '<validated options JSON>');
```

MySQL 参数用 `?`，PostgreSQL 用 `$1` 等；对象名不能当普通值参数替换。复杂过程统一参数 schema，但两端分别定义结果列类型。最终语法需要保持现有 `json_vector(...)`、ANN LIKE 与 lifecycle 命令兼容。

## 6. CI 与完成定义

| 层级 | 计划运行方式 | 必须覆盖 |
| --- | --- | --- |
| Unit / fuzz | 每个 PR | parser、类型转换、参数校验、计划、错误映射；`-race`、`go vet`、actionlint；statement coverage ≥90% |
| Core E2E | 每个 PR / main | 2.6.23 × mysql/postgres/both；2.6.2 原有能力回归；真实二进制和独立 Milvus |
| Feature E2E | 每个特性 PR；全量定时运行 | 每项类型/索引/过滤/搜索参数的正负例；对照官方 SDK；新增功能在不支持版本上明确失败 |
| Distributed / import / RBAC | 对应 PR 必跑；全量定时运行 | 多节点、对象存储、真实认证、权限撤销、异步失败、资源迁移 |
| GPU / provider / load | 专用 runner；对应特性发布前必跑 | 每个声明支持的硬件/模型组合及负载、故障验证 |

这些扩展 workflow 尚未实施；当前仍是 Readme 中的 2.6.2 测试。每个新阶段交付必须包含：实现、SQL 示例、权限/版本限制、双协议结果契约、单元测试、真实 E2E、review、CI 证据。

测试需分别核对数据值、字段类型、NULL/缺失、PK、计数、顺序/距离和状态；不能只检查请求无错误或结果非空。长任务用状态轮询与截止时间，禁止用固定 sleep 作为成功依据。失败上传脱敏日志/版本/配置；测试只清理自身唯一命名资源。

“完成比例”按登记的能力及参数合同计算，不使用代码覆盖率或已映射 RPC 数量代替。跳过专用环境的测试、仅 mock 通过、只实现一个协议，均不能记为已验收。

## 7. 首批 PR 拆分

| 顺序 | 可独立 review 的交付 | 前置 |
| --- | --- | --- |
| 1 | feature registry 初版、固定版本 SDK/API 参数盘点、2.6.23 E2E 基线 | 本 roadmap |
| 2 | 现代 Go SDK/adapter 迁移，保持现有 SQL 合同 | 1 |
| 3 | 协议中立结果/错误类型，绑定/NULL/复杂类型编码基础 | 2 |
| 4 | nullable/default + ARRAY/dynamic field，逐值 E2E | 3 |
| 5 | 稀疏/低精度/二进制向量与对应基础索引 | 3 |
| 6 | collection 属性、别名、schema 演进、分区路由 | 2–4 |
| 7 | partial upsert、完整过滤模板和 consistency | 4、6 |
| 8 | batch/range/iterator/grouping + hybrid 检索 | 5、7 |
| 9 | BM25/analyzer/function/rerank 与高级类型/索引分批交付 | P1/P4 相应基础 |
| 10 | 身份映射与 RBAC 后启用 import/resource/compaction 管理；最终 P8 | P0 身份设计、P2 |

后续 PR 按能力族继续拆分；每个 PR 保持实现与 E2E 同步。先补基础语义和接口，再扩索引/函数数量，避免错误的数据类型或身份模型扩散。
