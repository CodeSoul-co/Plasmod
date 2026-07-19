# Add A Materializer

实现流程：

1. 定义支持的 Event/object/target；
2. 解析 typed payload；
3. 计算 deterministic object/edge/version IDs；
4. 构建 canonical projection；
5. 通过 RuntimeStorage transaction 写入；
6. 请求 retrieval projection；
7. 将失败返回 consistency controller；
8. 注册到 app/runtime worker graph；
9. 添加 replay/retry/idempotency tests。

Materializer 不应自行更新 visible checkpoint；只有 runtime 确认所需阶段完成后 tracker 才推进。
