// create_test_engine.cpp — builds a minimal TRT engine for CI/integration testing.
//
// The engine simulates a BERT-like embedding model:
//   inputs : input_ids      INT32  [1, SEQ_LEN]
//            attention_mask INT32  [1, SEQ_LEN]
//   output : last_hidden_state FLOAT32 [1, SEQ_LEN, DIM]
//
// The output is the element-wise sum of a constant embedding table and a
// bias derived from input_ids, so the output depends on the input (required
// in TRT 10 kSTRONGLY_TYPED networks).
//
// Compile & run:
//   g++ -std=c++17 -O2 -I/usr/include/x86_64-linux-gnu \
//       -I/usr/local/cuda-12.9/include \
//       create_test_engine.cpp -o create_test_engine \
//       -lnvinfer -lcudart && ./create_test_engine

#include <NvInfer.h>
#include <cuda_runtime.h>
#include <fstream>
#include <iostream>
#include <vector>
#include <cstring>
#include <cmath>

static const int SEQ_LEN = 128;
static const int DIM     = 384;
static const int BATCH   = 1;

class SimpleLogger : public nvinfer1::ILogger {
public:
    void log(Severity sev, const char* msg) noexcept override {
        if (sev <= Severity::kWARNING)
            std::cerr << "[TRT] " << msg << "\n";
    }
} gLogger;

int main(int argc, char** argv) {
    const char* outPath = (argc > 1) ? argv[1]
                                     : "/home/duanzhenke/models/test_embed.engine";

    // ── Builder ──────────────────────────────────────────────────────────────
    auto* builder = nvinfer1::createInferBuilder(gLogger);
    if (!builder) { std::cerr << "createInferBuilder failed\n"; return 1; }

    // Default (non-strongly-typed) network with explicit batch
    const uint32_t flags = 0;
    auto* network = builder->createNetworkV2(flags);
    if (!network) { std::cerr << "createNetworkV2 failed\n"; return 1; }

    // ── Inputs ───────────────────────────────────────────────────────────────
    auto* inputIDs   = network->addInput("input_ids",
        nvinfer1::DataType::kINT32, nvinfer1::Dims2{BATCH, SEQ_LEN});
    auto* attnMask   = network->addInput("attention_mask",
        nvinfer1::DataType::kINT32, nvinfer1::Dims2{BATCH, SEQ_LEN});

    // ── Cast input_ids to FLOAT32 so we can do arithmetic ──────────────────
    auto* castIDs = network->addCast(*inputIDs, nvinfer1::DataType::kFLOAT);
    if (!castIDs) { std::cerr << "addCast(input_ids) failed\n"; return 1; }

    auto* castMask = network->addCast(*attnMask, nvinfer1::DataType::kFLOAT);
    if (!castMask) { std::cerr << "addCast(attention_mask) failed\n"; return 1; }

    // ── Embedding constant: shape [1, SEQ_LEN, DIM], values ~1/sqrt(DIM) ───
    const int total = BATCH * SEQ_LEN * DIM;
    std::vector<float> embedData(total);
    for (int i = 0; i < total; ++i)
        embedData[i] = 1.0f / std::sqrt(static_cast<float>(DIM));

    nvinfer1::Weights embedW{nvinfer1::DataType::kFLOAT,
                             embedData.data(), static_cast<int64_t>(total)};
    nvinfer1::Dims3 embedDims{BATCH, SEQ_LEN, DIM};
    auto* embedConst = network->addConstant(embedDims, embedW);
    if (!embedConst) { std::cerr << "addConstant failed\n"; return 1; }

    // ── Scale constant by mean of cast_ids (makes output depend on input) ──
    // Reshape castIDs [1,SEQ_LEN] → [1,SEQ_LEN,1] then broadcast-multiply
    nvinfer1::Dims3 reshapeDims{BATCH, SEQ_LEN, 1};
    auto* reshape = network->addShuffle(*castIDs->getOutput(0));
    if (!reshape) { std::cerr << "addShuffle failed\n"; return 1; }
    reshape->setReshapeDimensions(reshapeDims);

    // Elementwise multiply: [1,SEQ_LEN,DIM] * [1,SEQ_LEN,1] → [1,SEQ_LEN,DIM]
    auto* scaled = network->addElementWise(
        *embedConst->getOutput(0),
        *reshape->getOutput(0),
        nvinfer1::ElementWiseOperation::kPROD);
    if (!scaled) { std::cerr << "addElementWise failed\n"; return 1; }

    // ── Mark output ──────────────────────────────────────────────────────────
    auto* output = scaled->getOutput(0);
    output->setName("last_hidden_state");
    network->markOutput(*output);

    // ── Build config ─────────────────────────────────────────────────────────
    auto* config = builder->createBuilderConfig();
    config->setMemoryPoolLimit(nvinfer1::MemoryPoolType::kWORKSPACE, 256UL << 20);

    std::cout << "Building TRT engine (this may take a minute)...\n";
    auto* serialized = builder->buildSerializedNetwork(*network, *config);
    if (!serialized) { std::cerr << "buildSerializedNetwork failed\n"; return 1; }

    // ── Write to file ─────────────────────────────────────────────────────────
    std::ofstream f(outPath, std::ios::binary);
    if (!f.good()) {
        std::cerr << "Cannot open output file: " << outPath << "\n";
        return 1;
    }
    f.write(static_cast<const char*>(serialized->data()), serialized->size());
    f.close();

    std::cout << "Engine saved to: " << outPath << "\n";
    std::cout << "  Size  : " << serialized->size() << " bytes\n";
    std::cout << "  Inputs: " << network->getNbInputs() << "\n";
    std::cout << "  Outputs: " << network->getNbOutputs() << "\n";

    delete serialized;
    delete config;
    delete network;
    delete builder;
    return 0;
}
