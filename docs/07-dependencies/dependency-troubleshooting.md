# Dependency Troubleshooting

## CGO/native library not found

检查 `cpp/build`、build tag、`CGO_ENABLED`、动态库依赖和 rpath。若只需 canonical 功能，可明确使用 pure Go
build，而不是伪造 native 成功。

## FAISS/OpenMP errors

确认 CMake option、architecture 和 package manager prefix 一致。Apple Silicon 不要混用 x86_64 library。

## Badger lock

确认没有第二个进程使用同一数据目录；检查 Docker 和本地进程。不要删除 LOCK 文件绕过真实并发写入。

## MinIO connection

区分 API 9000 和 console 9001；检查 endpoint scheme、TLS、bucket、credential 和 container DNS。

## Embedding mismatch

查看 model ID、dimension 和 segment embedding family。更换模型后执行受控 reindex，不要继续写入旧 segment。
