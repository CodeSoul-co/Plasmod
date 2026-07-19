# Upstream Compatibility Areas

以下目录应先阅读其 license/source map 再修改：

- `src/internal/platformpkg`；
- `src/internal/coordinator/controlplane`；
- `src/internal/eventbackbone/streamplane`；
- `cpp/vendor`。

维护要求：保留 copyright/license；记录上游版本；把 Plasmod adapter 放在边界层；避免无关格式化导致巨大
diff；升级后执行 active startup、storage、retrieval 和 shutdown 验证。
