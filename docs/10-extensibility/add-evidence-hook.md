# Add An Evidence Hook

Evidence hook 可以补充 GraphNode、Edge、ProofStep、provenance 或过滤说明。

要求：

- 输入只使用当前查询允许的对象；
- 输出 deterministic，可限制节点/边数量；
- 遵守 context cancel/timeout；
- 不修改 canonical source；
- 不通过描述文本泄露被拒绝对象；
- failure 语义明确为 fail query、omit hook 或 mark partial；
- 在 production visibility middleware 前返回 typed fields。

新增 hook 要补 query/evidence tests，并在 response contract 中标明稳定性。
