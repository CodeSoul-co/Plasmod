# Embedder Providers

DataPlane 通过 `EmbeddingGenerator`/`embedding.Generator` 生成向量，支持：

- TF-IDF：无外部模型，适合基础运行；
- ONNX local model：需要 model/tokenizer/runtime；
- 代码中已接入的其他 provider；
- precomputed vector：Event/Query 直接提供。

## Compatibility tuple

每个向量空间至少由以下信息标识：

```text
embedding family + model ID + dimension + normalization/metric
```

任一项变化都可能要求 reindex。只比较 dimension 会把不兼容向量混入同一 segment。

## Failure behavior

Embedding 失败不应悄悄写零向量。调用者可显式选择 lexical-only/skip-vector，或让 strict projection 返回失败。
