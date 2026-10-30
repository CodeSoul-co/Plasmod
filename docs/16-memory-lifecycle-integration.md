# 16. 四条 Chain 与伙伴记忆接入契约

本章对应 2026-10-30 的接入审计修复。范围是 Plasmod 核心库；Shenfer 的登录身份、来源许可、授权与版本固定仍由接入方维护。

## 实际入口

| 入口 | 执行路径 | 返回/持久化 |
|---|---|---|
| `/v1/ingest/events`、`/v1/ingest/document` | Runtime → `MainChain.Submit` → consistency/WAL → `MainChain.Project` | canonical → MemoryPipelineChain 治理 → retrieval；失败不推进可见水位 |
| `/v1/internal/memory/{ingest,update,decay,compress,summarize,maintenance}` | scoped selection → `MemoryPipelineChain.RunAlgorithm` | durable operation receipt、algorithm state、Memory、derived Event/Version/Edge/index |
| `/v1/query` | retrieval → `MemoryPipelineChain.RunAlgorithm(recall)` → `QueryChain.Run` | canonical scope/forgotten filter、排序、proof/evidence |
| share / conflict decision | `CollaborationChain.Share` / `Decide` | share 复用 WAL；显式冲突决议保存状态、版本和关系 |
| EventSubscriber | MainChain 的 State/tool 辅助阶段；MemoryPipelineChain 维护；CollaborationChain 合并 | MemoryBank 模式不再调用 baseline 的相邻消息合并/压缩规则 |
| Orchestrator | bootstrap `BindRuntime` → 同一 Runtime/Chain | 无效 payload/失败计入 Failed，不计 Completed |

主请求同步调用 Runtime 所拥有的 Chain，不必排入可选的优先级队列。consistency controller 仍负责 WAL 顺序、水位与重试。`MainChain.Run` 等旧的 worker-only 入口保留供兼容和独立测试；生产队列绑定 Runtime。管理用 canonical CRUD 仍是直接存储 API，不具有完整 Event 工作流语义。

## 维护与候选晋升

```json
POST /v1/internal/memory/maintenance
{
  "operation_id": "cycle-001",
  "operation": "update",
  "tenant_id": "user-1",
  "workspace_id": "partner-1",
  "agent_id": "agent-1",
  "memory_ids": ["mem_evt_1"],
  "signals": {"mem_evt_1": 1}
}
```

`signals[id] >= 1` 表示接入方传入的**明确用户确认**，不能为普通消息统一设置。普通文本保持 candidate；时间流逝本身不能把 candidate 转为可召回 stale。事实/偏好提炼后，相同规范文本在至少三个独立会话、三个来源事件中重复、置信度不低于 0.7 且没有噪声/严重冲突，才由 MemoryBank 晋升。其他内容仍由配置的准入分数处理。不会在接入方重写分数。

- 必填 `tenant_id`、`workspace_id`。HTTP 未给 operation_id 时生成一个，推荐写入方明确提供；也接受 `Idempotency-Key`。
- `agent_id`、`session_id`、`memory_ids` 可选，所有约束取交集。无 ID 时从 canonical store 按范围选取；跨范围 ID 返回通用跳过原因。
- `operation`：ingest、update、decay、compress、summarize；decay 可传 RFC3339 `now_ts`。
- `limit` 默认 100，上限 1000；按 Memory ID 排序，使用 `next_cursor` 作为下次 `after_id`，为每页分配不同 operation_id。`has_more=false` 完成本次扫描。
- 回执：`selected_count`（范围内选中）、`updated_count`、`skipped_count`、`failed_count`、`items[].reason/before/after/source_event_ids`、`produced_ids`。
- `GET /v1/internal/memory/operations/{operation_id}?tenant_id=...&workspace_id=...` 查询持久回执。
- 相同 ID/请求不重复强化或生成摘要；相同 ID/不同请求被拒绝。处理中断的维护被报告为 partial，明确标记未知执行边界，不盲目重复强化。先查询对象/回执，再以新操作继续。

新增匹配证据会在摄入阶段触发候选复评。MemoryBank 的 subscriber 每十个 agent/session 事件触发一轮分页 decay；游标和各页回执持久化，WAL 回放复用同一操作 ID。无新消息期间可由外部定时调用 maintenance/decay。调度触发器不拥有候选选择、准入或衰减公式。

## 派生对象与失败边界

压缩/摘要按 tenant、workspace、agent、session、完整 access、TTL/到期时间分组，不把不同权限的来源合并成更宽权限对象。产物保存身份、来源事件并集、源 Memory 的 derived_from 关系、Event、Version、embedding 引用及检索投影；源算法状态和生命周期在产物索引成功后才保存。

canonical 与 ANN 不宣称跨组件 ACID。投影失败返回 partial；源记忆保持原状态。MemoryBank 派生 ID 由操作和来源版本确定，重试复用已经持久化的内容。启动时重建索引使用持久 segment/canonical 数据。历史版本不是当前事实。

## 更正、遗忘

```json
POST /v1/internal/memory/mutate
{
  "operation_id": "forget-001",
  "operation": "forget",
  "tenant_id": "user-1",
  "workspace_id": "partner-1",
  "reference_ids": ["evt_1"]
}
```

`operation=correct` 另传 `replacement`（v0.4 Event）。替代 Event ID 由操作 ID 确定；不会继承已撤回来源的派生依赖。新内容仍须经过准入。

`GET /v1/internal/memory/mutations/{operation_id}?tenant_id=...&workspace_id=...` 返回 running/partial/completed、forgotten_ids、replacement_id 和 error。partial 可用同一请求继续；不是伪装的原子更正。

范围内按 source_event_ids 与 derived_from/caused_by 追踪。持久遗忘标记保护原文、派生 Memory、依赖 State/evidence 和旧版本；query/evidence/WAL replay 都检查。trace 对已遗忘对象返回 410。新派生不得引用已遗忘来源。该接口是**逻辑遗忘**，不是磁盘/WAL/备份的物理擦除；物理清理由受控 purge/备份保留策略处理。

旧 admin delete-by-source 保留兼容；接入方应切换统一 mutation 才获得操作回执、来源闭包及回放屏障。

## 语义提炼、画像和冲突

环境配置：

```sh
PLASMOD_MEMORY_LLM_BASE_URL=https://your-provider.example/v1
PLASMOD_MEMORY_LLM_MODEL=your-model
PLASMOD_MEMORY_LLM_API_KEY= # 通过部署环境注入
```

Plasmod 使用自身 provider 调用 chat completions，单次超时 60 秒，不自动重试 HTTP 调用。`user_message` 提炼只接收事实/持久偏好；每条结果必须有可验证的 Unicode 原文范围、精确引文、置信度和可选有效时间。问题、引用、助手猜测、临时意图、遗忘指令不应提炼为事实。来源校验不等于模型语义正确性保证，模型质量另行验收。

语义摘要必须逐结论引用输入来源；未知引用、空结果和格式错误会失败，不静默回退。未配置 provider 时保留基础 materialization 和明确标记 `summary_method=extractive` 的算法摘要，不宣称已完成语义压缩。

提炼可给出显式属性 `key`；同属性不同事实进入冲突机制，偏好变化不直接视为事实矛盾。`POST /v1/internal/memory/conflict/decision` 接收 operation_id、tenant_id、workspace_id、winner_id、loser_id、kind（factual_conflict / preference_change）。显式决议保存有效时间、旧证据、版本、关系和治理状态；重复请求幂等。

已准入的 profile/preference 生成 `StateType=inferred_profile`，含来源 ID、版本及 inferred 标记。它不会修改 Agent.RoleProfile 或覆盖用户明确配置。画像/冲突 State 可通过带 scope 的 `/v1/states` 读取。

## 文档与 SDK

`/v1/ingest/document` 支持 tenant_id、workspace_id、agent_id、session_id、source_id、idempotency_key（或请求头）、text、chunk_size、overlap、upload_batch_id、segment_index、segment_total。

- 分段和 manifest 写入 canonical State，重启后可继续；同范围/来源/分段号内容变化返回 409。
- 缺段返回 accumulating/missing_segments；完成后每 chunk 的 Event ID 固定，重复提交不新增对象。
- chunk 保留 Unicode 原文起止位置，返回每段 ACK/error 和 failed_count；失败不能冒充全成功。
- source_id 可作为统一遗忘的 reference_id；租户/agent/session 相同的上传才会组装。
- 兼容旧的无 tenant 文档请求，但多用户接入必须填写完整 scope。

Node SDK 0.2 提供 PlasmodClient（AndbClient 别名兼容）、TypeScript 声明、摄入/query/recall/evidence、对象/State、maintenance、mutation、回执查询；支持请求 header 和 AbortSignal。传输失败对写操作标记 effectUnknown，不内置未知写入重试。详见 `sdk/nodejs/README.md`。

## 验收与未声明事项

自动化用例覆盖 Chain 顺序/失败、实际 HTTP 路径、候选/确认/跨会话重复、摘要身份/版本/来源/检索、隔离、遗忘、修正幂等、磁盘重启、文档分段、provider 引文校验、SDK 不重试以及队列失败计数。

真实模型效果、生产 Shenfer 固定版本升级与回归、真实 S3/Cold 恢复另行验收。当前不宣称通用语义冲突自动裁决、跨进程分布式维护锁、端到端 exactly-once 外部模型调用、所有管理 API 的用户登录认证，或全量物理擦除。
