# Python SDK Quickstart

## 安装

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e ./sdk/python
```

当前包名为 `plasmod-sdk`，导入模块为 `plasmod_sdk`，客户端类为 `PlasmodClient`。

## 写入和查询

```python
from plasmod_sdk import PlasmodClient

client = PlasmodClient(base_url="http://127.0.0.1:8080")

client.ingest_event(
    event_id="evt_python_001",
    agent_id="agent-python",
    session_id="session-python",
    event_type="user_message",
    payload={"text": "The user prefers concise answers."},
    tenant_id="tenant-quickstart",
    workspace_id="workspace-quickstart",
    access={"consistency": "strict", "visibility": "workspace"},
)

result = client.query(
    query_text="answer style preference",
    tenant_id="tenant-quickstart",
    workspace_id="workspace-quickstart",
    session_id="session-python",
    agent_id="agent-python",
    top_k=10,
)
print(result)
```

SDK 是 HTTP 客户端，不会在本地启动 Plasmod，也不会替服务端生成持久化目录。更完整的方法签名见
[`../05-api-and-reference/sdk-reference.md`](../05-api-and-reference/sdk-reference.md)。
