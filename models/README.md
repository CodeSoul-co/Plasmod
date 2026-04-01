# Models

Binary model files are **not committed** (see `.gitignore`). Download them locally before running ONNX or TensorRT embedders.

## all-MiniLM-L6-v2 (384-dim, BERT-base tokenizer)

### Download via hf-mirror (recommended in CN)

```bash
mkdir -p models
# ONNX model (~86 MB)
wget -O models/minilm-l6-v2.onnx \
  "https://hf-mirror.com/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx"

# BERT vocab (30522 tokens)
wget -O models/minilm-l6-v2-vocab.txt \
  "https://hf-mirror.com/sentence-transformers/all-MiniLM-L6-v2/resolve/main/vocab.txt"
```

### Download via ModelScope

```bash
pip install modelscope
python3 -c "
from modelscope import snapshot_download
snapshot_download('sentence-transformers/all-MiniLM-L6-v2', local_dir='models/minilm-l6-v2')
"
```

### Convert ONNX → TensorRT engine (FP16, seq_len=128)

```bash
# Build libandb_tensorrt.so first
make tensorrt

# Convert (requires trtexec from libnvinfer-bin)
trtexec \
  --onnx=models/minilm-l6-v2.onnx \
  --saveEngine=models/minilm-l6-v2-fp16.engine \
  --fp16 \
  --minShapes=input_ids:1x1,attention_mask:1x1,token_type_ids:1x1 \
  --optShapes=input_ids:1x128,attention_mask:1x128,token_type_ids:1x128 \
  --maxShapes=input_ids:4x128,attention_mask:4x128,token_type_ids:4x128
```

### Environment variables

```bash
# ONNX embedder
export ANDB_EMBEDDER=onnx
export ANDB_EMBEDDER_MODEL_PATH=models/minilm-l6-v2.onnx
export ANDB_ONNX_VOCAB_PATH=models/minilm-l6-v2-vocab.txt
export ANDB_EMBEDDER_DIM=384

# TensorRT embedder
export ANDB_EMBEDDER=tensorrt
export ANDB_EMBEDDER_MODEL_PATH=models/minilm-l6-v2-fp16.engine
export ANDB_ONNX_VOCAB_PATH=models/minilm-l6-v2-vocab.txt
export ANDB_EMBEDDER_DIM=384
```
