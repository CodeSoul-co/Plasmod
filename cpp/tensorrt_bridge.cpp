// TensorRT C++ Bridge for Go CGO
// Compatible with TensorRT 8.x / 9.x / 10.x
// In TRT 10+: destroy() is removed (use delete), getNbBindings() is removed,
//             enqueueV2 replaced by enqueueV3.

#include <NvInfer.h>
#include <cuda_runtime.h>
#include <fstream>
#include <iostream>
#include <vector>
#include <cstring>

// TRT 10 removes the destroy() virtual method; use delete instead.
// Detect by checking for the macro set in NvInferVersion.h.
#if NV_TENSORRT_MAJOR >= 10
#  define TRT_DESTROY(obj) delete (obj)
#else
#  define TRT_DESTROY(obj) (obj)->destroy()
#endif

// Logger for TensorRT
class Logger : public nvinfer1::ILogger {
public:
    void log(Severity severity, const char* msg) noexcept override {
        if (severity <= Severity::kWARNING) {
            std::cerr << "[TensorRT] " << msg << std::endl;
        }
    }
} gLogger;

// TensorRT engine handle
typedef struct {
    nvinfer1::IRuntime*          runtime;
    nvinfer1::ICudaEngine*       engine;
    nvinfer1::IExecutionContext* context;
    cudaStream_t                 stream;
    int                          numIOTensors;  // TRT10: num IO tensors; TRT8: numBindings
} TRTEngine;

extern "C" {

// Load TensorRT engine from a serialised .engine file.
TRTEngine* trt_load_engine(const char* engine_path) {
    if (!engine_path) {
        std::cerr << "[TensorRT] Engine path is NULL" << std::endl;
        return nullptr;
    }

    std::ifstream file(engine_path, std::ios::binary);
    if (!file.good()) {
        std::cerr << "[TensorRT] Failed to open engine file: " << engine_path << std::endl;
        return nullptr;
    }

    file.seekg(0, std::ios::end);
    size_t size = file.tellg();
    file.seekg(0, std::ios::beg);
    std::vector<char> engineData(size);
    file.read(engineData.data(), size);
    file.close();

    if (size == 0) {
        std::cerr << "[TensorRT] Engine file is empty" << std::endl;
        return nullptr;
    }

    nvinfer1::IRuntime* runtime = nvinfer1::createInferRuntime(gLogger);
    if (!runtime) {
        std::cerr << "[TensorRT] Failed to create runtime" << std::endl;
        return nullptr;
    }

    nvinfer1::ICudaEngine* engine =
        runtime->deserializeCudaEngine(engineData.data(), size);
    if (!engine) {
        std::cerr << "[TensorRT] Failed to deserialize engine" << std::endl;
        TRT_DESTROY(runtime);
        return nullptr;
    }

    nvinfer1::IExecutionContext* context = engine->createExecutionContext();
    if (!context) {
        std::cerr << "[TensorRT] Failed to create execution context" << std::endl;
        TRT_DESTROY(engine);
        TRT_DESTROY(runtime);
        return nullptr;
    }

    cudaStream_t stream;
    if (cudaStreamCreate(&stream) != cudaSuccess) {
        std::cerr << "[TensorRT] Failed to create CUDA stream" << std::endl;
        TRT_DESTROY(context);
        TRT_DESTROY(engine);
        TRT_DESTROY(runtime);
        return nullptr;
    }

    TRTEngine* trt      = new TRTEngine();
    trt->runtime        = runtime;
    trt->engine         = engine;
    trt->context        = context;
    trt->stream         = stream;

#if NV_TENSORRT_MAJOR >= 10
    trt->numIOTensors   = engine->getNbIOTensors();
    std::cout << "[TensorRT] Engine loaded (TRT 10+): " << engine_path
              << "  io_tensors=" << trt->numIOTensors << std::endl;
#else
    trt->numIOTensors   = engine->getNbBindings();
    std::cout << "[TensorRT] Engine loaded: " << engine_path
              << "  bindings=" << trt->numIOTensors << std::endl;
#endif

    return trt;
}

// Execute TensorRT inference.
// `bindings` is an array of device pointers with one entry per IO tensor/binding.
int trt_execute_inference(TRTEngine* trt, void** bindings) {
    if (!trt || !trt->context) {
        std::cerr << "[TensorRT] Invalid engine or context" << std::endl;
        return -1;
    }

    bool success = false;

#if NV_TENSORRT_MAJOR >= 10
    // TRT10+: set tensor addresses and enqueueV3
    int n = trt->engine->getNbIOTensors();
    for (int i = 0; i < n; ++i) {
        const char* name = trt->engine->getIOTensorName(i);
        trt->context->setTensorAddress(name, bindings[i]);
    }
    success = trt->context->enqueueV3(trt->stream);
#else
    success = trt->context->enqueueV2(bindings, trt->stream, nullptr);
#endif

    if (!success) {
        std::cerr << "[TensorRT] Inference execution failed" << std::endl;
        return -1;
    }

    cudaError_t err = cudaStreamSynchronize(trt->stream);
    if (err != cudaSuccess) {
        std::cerr << "[TensorRT] Stream sync failed: "
                  << cudaGetErrorString(err) << std::endl;
        return -1;
    }

    return 0;
}

// Free TensorRT engine resources.
void trt_free_engine(TRTEngine* trt) {
    if (!trt) return;

    if (trt->stream)   cudaStreamDestroy(trt->stream);
    if (trt->context)  TRT_DESTROY(trt->context);
    if (trt->engine)   TRT_DESTROY(trt->engine);
    if (trt->runtime)  TRT_DESTROY(trt->runtime);

    delete trt;
    std::cout << "[TensorRT] Engine resources freed" << std::endl;
}

} // extern "C"
