# Add A Policy Rule

Policy rule 输入应包含 object、actor/scope、operation、PolicyRecord/ShareContract 和 runtime context；输出包含
allow/deny/quarantine/weight/TTL 等明确 decision。

实现步骤：

1. 定义 rule ID/version；
2. 添加 typed config；
3. 在读/写正确 hook point 执行；
4. 记录 PolicyRecord/decision reason/source/event ID；
5. Evidence 中只暴露允许内容；
6. 测试 deny 优先级、冲突规则和默认失败策略；
7. 将配置加入 effective config（脱敏）。

安全规则异常时默认行为必须显式，不能无意 fail-open。
