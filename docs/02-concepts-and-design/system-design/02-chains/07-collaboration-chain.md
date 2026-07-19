# 7. Collaboration Chain

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Chain |
| 目标 | 支持 agent 间 share、conflict resolution、handoff 和 aggregate |
| 关键路径 | internal collaboration operations 可达；不在普通 Event/Query 必经路径 |
| 成熟度 | 部分 |

## 2. Entry points

| Entry | Concrete call |
|---|---|
| Chain | `CollaborationChain.Run(CollaborationChainInput)` |
| Runtime | `DispatchShare`, `DispatchConflictResolve` |
| HTTP | memory share/conflict, agent handoff, MAS aggregate/consistency, agent list |
| Workers | ConflictMergeWorker, CommunicationWorker, MicroBatchScheduler |
| Stores | ObjectStore, EdgeStore, ShareContractStore, PolicyStore |

## 3. Input/output

| Operation | Input | Output | Mutation |
|---|---|---|---|
| conflict merge | left/right Memory IDs, object type | winner/loser | loser inactive + conflict edge |
| share/broadcast | from/to agent + Memory ID | shared Memory ID | copied Memory with target agent/provenance |
| CollaborationChain | conflict input + agent IDs | winner/shared IDs | merge, microbatch enqueue, optional broadcast |
| handoff | source/target/session/task context | handler response | Event/share path depending body |
| aggregate/consistency | agent answers/results | score/aggregate | mainly response/metrics |

## 4. Scope and contract behavior

| Concern | Current behavior |
|---|---|
| Agent/session resolution | request fields and Memory fields |
| Share contract storage | canonical CRUD exists |
| Read ACL | contamination detector checks matching contract; not universal pre-read deny |
| Write/derive ACL | schema exists; no central enforcement on every collaboration mutation |
| Shared object model | creates a copied Memory `shared_<source>_to_<target>` |
| Handoff event | internal handler may submit Event; not a single canonical collaboration transaction |
| Conflict detection | same agent+session active Memories; LWW by Version |
| Conflict preservation | loser remains stored but inactive, edge points winner -> loser |

## 5. Chain and API relationship

`CollaborationChain.Run` 先 merge，再把 result 放入 in-memory microbatch，最后 broadcast。实际 internal HTTP share/conflict 多数直接调用 Runtime/NodeManager，而不调用 CollaborationChain。Microbatch 没有后台定时 flush，enqueue 不表示后续 fan-out 已处理。

## 6. Data/state

- Canonical：source/shared Memory、conflict Edge、ShareContract/Policy records。
- Provenance：shared Memory 的 `ProvenanceRef=shared_from:<agent>/<memory>`；conflict edge 当前不总带 provenance ref。
- Version：share/conflict worker 不统一创建 ObjectVersion。
- Projection：shared/winner mutation 不统一 reindex。
- WAL：直接 share/conflict 不统一写 Event/WAL。

## 7. Correctness and failure

- Share source missing 或 same-agent 时是 no-op，不一定返回 error。
- Conflict precondition不满足时是 no-op；LWW 相同 version 选择 left。
- Memory copy、Edge、Version、projection、audit 不在统一 transaction。
- Cross-agent contamination 当前是 metrics detection，不能替代 prevention。
- Scope/ACL/contract policy 对 direct canonical routes也未统一 enforce。

## 8. 声明边界

可声明：显式 ShareContract schema/store、shared Memory copy、same-session LWW conflict preservation、handoff/MAS internal adapter。

不可声明：完整 multi-agent transaction、全面 ACL/derive enforcement、自动 semantic conflict detection、统一 merge policy plugin 或跨 agent zero-leak guarantee。

## 9. 缺口

1. CollaborationCommand 经 policy authorize 后写 derived Event/WAL；
2. share/merge 必须一起写 Version/Edge/Audit/projection；
3. contract read/write/derive/merge policy 统一 evaluator；
4. semantic conflict detector 和 pluggable merge strategy；
5. durable microbatch/job status；
6. prevention-based contamination tests。
