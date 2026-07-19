# Add A Query Operator

1. 在 QueryRequest 增加 typed optional field 或 `query_ops` descriptor；
2. semantic planner 解析；
3. 对 Hot、Warm、Cold 和 canonical supplement 定义一致语义；
4. 在 policy/filter 之前或之后的位置写清楚；
5. 返回 `applied_filters`/proof 信息；
6. 批查询保持等价；
7. 更新 Python SDK；
8. 测试空值、组合、scope leak 和 unsupported backend。

不能只在 ANN 结果上实现过滤，否则 canonical supplement 或 Cold 可能绕过条件。
