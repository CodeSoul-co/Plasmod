# Licenses And Attribution

## Required checks

- 根仓库 LICENSE；
- `src/internal/platformpkg/UPSTREAM_LICENSE`；
- `cpp/vendor` 内各第三方 license；
- Go module licenses；
- FAISS、DiskANN、HNSW、ONNX Runtime、Badger 等分发要求。

## Distribution

发布 source、binary 或 Docker image 前：

1. 生成 dependency inventory；
2. 保留 copyright 和 license text；
3. 标记修改过的上游代码；
4. 检查静态/动态链接是否改变义务；
5. 将 notices 放入发布物；
6. 不把内部来源说明当作法律结论。

新增 vendor 代码必须在 merge 前完成来源和 license 审核。
