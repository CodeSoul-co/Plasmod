# Retrieval Schema

## Event Retrieval

Event 的 retrieval group 描述：namespace、index text、embedding presence/dimension/vector，以及 materialized
retrieval 状态。它是 canonical Event 的投影指令，不是 canonical object 本身。

## RetrievalSegment

字段包括 segment ID、object type、namespace、time bucket、embedding family、storage/index ref、row count、
min/max timestamp 和 tier。

## 物理索引

原生层接受 HNSW、IVF_FLAT、IVF_PQ、IVF_SQ8、DISKANN。Embedding family、dimension 和 model ID 必须
作为 segment 兼容边界；同维度不代表同 embedding 空间。

## Query Projection

Query 可以提供 `embedding_vector` 绕过 embedder。若为空，数据面可调用 configured embedder。结果还会与
lexical/canonical 候选和 Evidence 组装合并，因此原生 ANN hit 不是最终 QueryResponse 的全部语义。
