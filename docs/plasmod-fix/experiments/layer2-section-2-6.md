# 第二层实验方法（Plasmod.md §2.6）— Member A 接口实测手册

本手册用于直接测试 `Plasmod.md` 中 Member A 相关接口（部署/鉴权/数据安全、wipe/purge、replay/recovery、S3 冷层边界）。

---

## 1. 测试范围（Member A 相关）

本次覆盖以下接口：

- `GET /healthz`
- `GET /v1/admin/storage`
- `GET /v1/admin/topology`
- `GET/POST /v1/admin/consistency-mode`
- `POST /v1/admin/replay`（preview + apply）
- `POST /v1/admin/rollback`
- `POST /v1/admin/dataset/delete`
- `POST /v1/admin/dataset/purge`
- `POST /v1/admin/data/wipe`
- `POST /v1/admin/s3/cold-purge`

---

## 2. 前置准备（终端 A）

在仓库根目录执行：

```bash
export BASE_URL="http://127.0.0.1:8080"
export ADMIN_KEY="your_admin_key"

# 推荐新变量；旧变量 ANDB_ADMIN_API_KEY 也兼容
export PLASMOD_ADMIN_API_KEY="$ADMIN_KEY"

# 启动服务（按你的实际启动方式）
make dev
```

如果你使用 Docker，请确保端口映射到本机 `8080`，并把 `BASE_URL` 改为对应地址。

---

## 3. 通用调用模板（终端 B）

```bash
alias acurl='curl -sS -H "Content-Type: application/json" -H "X-Admin-Key: '"$ADMIN_KEY"'"'
```

后续示例都默认在仓库根目录执行，且 `BASE_URL`、`ADMIN_KEY` 已设置。

---

## 4. 快速健康与鉴权验证

### 4.1 健康检查

```bash
curl -sS "$BASE_URL/healthz"
```

预期：返回 `{"status":"ok"}`。

### 4.2 管理面鉴权检查

无 key：

```bash
curl -sS "$BASE_URL/v1/admin/storage"
```

带 key：

```bash
acurl "$BASE_URL/v1/admin/storage"
```

预期：
- 启用了 admin key 时：无 key 请求应 `401`，带 key 应成功。
- 未启用 admin key 时：两者都可能成功（dev 默认）。

---

## 5. §2.6 实验一：写入速率阶梯

```bash
python3 docs/plasmod-md/tools/layer2_exp26.py --base-url "$BASE_URL" exp1 \
  --ladder "1,4,8,16" \
  --step-seconds 20 \
  --w2v-probes 8 \
  --json-out docs/plasmod-md/experiments/out-exp1.json
```

关注指标（对应 §2.5）：
- `e2e_write_to_visible_ms`
- `query_latency_ms_under_write_load`
- `stale_probe`

---

## 6. §2.6 实验二：一致性模式 + 控制面接口

### 6.1 查询当前模式

```bash
acurl "$BASE_URL/v1/admin/consistency-mode"
```

### 6.2 切换模式并验证

```bash
acurl -X POST "$BASE_URL/v1/admin/consistency-mode" -d '{"mode":"strict_visible"}'
acurl -X POST "$BASE_URL/v1/admin/consistency-mode" -d '{"mode":"bounded_staleness"}'
acurl -X POST "$BASE_URL/v1/admin/consistency-mode" -d '{"mode":"eventual_visibility"}'
```

### 6.3 在固定档位做基线复测

```bash
python3 docs/plasmod-md/tools/layer2_exp26.py --base-url "$BASE_URL" exp2 --baseline-ladder-step 8 \
  | tee docs/plasmod-md/experiments/out-exp2.txt
```

说明：当前实现中模式主要用于控制面编排与实验记录，查询路径仍是单路径行为；报告中请明确写出这点。

---

## 7. §2.6 实验三：恢复与重放（含 wipe）

> 破坏性操作：会清空数据。

```bash
python3 docs/plasmod-md/tools/layer2_exp26.py --base-url "$BASE_URL" exp3 \
  --golden-n 80 \
  --i-understand-wipe \
  --json-out docs/plasmod-md/experiments/out-exp3.json
```

你也可以直接手工验证 replay：

### 7.1 replay 预览

```bash
acurl -X POST "$BASE_URL/v1/admin/replay" -d '{
  "from_lsn": 0,
  "limit": 200,
  "dry_run": true
}'
```

### 7.2 replay 执行（apply）

```bash
acurl -X POST "$BASE_URL/v1/admin/replay" -d '{
  "from_lsn": 0,
  "limit": 200,
  "apply": true,
  "confirm": "apply_replay"
}'
```

---

## 8. Member A 运维安全链路专项（手工接口测试）

下面是一组建议按顺序执行的命令，用于验证删除/回滚/全清/冷层边界。

### 8.1 软删除（dataset delete）

```bash
acurl -X POST "$BASE_URL/v1/admin/dataset/delete" -d '{
  "workspace_id":"ws_member_a",
  "dataset_name":"exp_member_a",
  "dry_run":true
}'
```

确认命中后执行真实删除：

```bash
acurl -X POST "$BASE_URL/v1/admin/dataset/delete" -d '{
  "workspace_id":"ws_member_a",
  "dataset_name":"exp_member_a",
  "dry_run":false
}'
```

### 8.2 硬删除（dataset purge）

```bash
acurl -X POST "$BASE_URL/v1/admin/dataset/purge" -d '{
  "workspace_id":"ws_member_a",
  "dataset_name":"exp_member_a",
  "only_if_inactive":true,
  "dry_run":true
}'
```

真实执行：

```bash
acurl -X POST "$BASE_URL/v1/admin/dataset/purge" -d '{
  "workspace_id":"ws_member_a",
  "dataset_name":"exp_member_a",
  "only_if_inactive":true,
  "dry_run":false
}'
```

### 8.3 rollback（reactivate/deactivate）

```bash
acurl -X POST "$BASE_URL/v1/admin/rollback" -d '{
  "memory_id":"mem_xxx",
  "action":"deactivate",
  "dry_run":true,
  "reason":"member_a_test"
}'
```

真实执行：

```bash
acurl -X POST "$BASE_URL/v1/admin/rollback" -d '{
  "memory_id":"mem_xxx",
  "action":"reactivate",
  "dry_run":false,
  "reason":"member_a_recover"
}'
```

### 8.4 全清（wipe）

```bash
acurl -X POST "$BASE_URL/v1/admin/data/wipe" -d '{
  "confirm":"delete_all_data"
}'
```

### 8.5 冷层清理边界（s3 cold purge）

```bash
acurl -X POST "$BASE_URL/v1/admin/s3/cold-purge" -d '{
  "confirm":"purge_cold_tier",
  "dry_run":true
}'
```

真实执行：

```bash
acurl -X POST "$BASE_URL/v1/admin/s3/cold-purge" -d '{
  "confirm":"purge_cold_tier",
  "dry_run":false
}'
```

说明：若是 S3/MinIO 冷层，响应通常会提示 bucket 生命周期或手工清理边界，这属于预期行为。

---

## 9. 推荐双终端执行方式

- 终端 A：运行服务（`make dev`）并观察日志。
- 终端 B：执行本手册中的 `curl` 与 `layer2_exp26.py` 命令。

这样可以同时看到：
- 客户端响应（终端 B）
- 服务端行为与异常日志（终端 A）

---

## 10. 结果记录模板（建议直接贴到实验报告）

记录以下字段：

- 环境：机器/OS/Go 版本/存储模式（memory 或 badger）/是否 S3
- 鉴权：`PLASMOD_ADMIN_API_KEY` 是否启用
- exp1：各档位 `p95_w2v`、`p95_query`、`stale_rate`
- exp2：模式切换返回值 + 固定档位复测结果
- exp3：`recovery_time`、replay preview/apply 摘要
- 运维链路：delete/purge/rollback/wipe/cold-purge 的响应与结论

结论建议至少回答：
1. 成员 A 负责接口是否都可调用；
2. 破坏性接口是否具备确认机制；
3. 恢复接口（replay/rollback）是否满足预期；
4. S3 冷层边界是否被明确暴露。
