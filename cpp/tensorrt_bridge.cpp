// TensorRT C++ Bridge for Go CGO
// This file provides the actual TensorRT engine loading and inference execution
// that replaces the stubs in tensorrt_cuda.go

#include <NvInfer.h>
#include <cuda_runtime.h>
#include <fstream>
#include <iostream>
#include <vector>
#include <cstring>

// Logger for TensorRT
class Logger : public nvinfer1::ILogger {
public:
    void log(Severity severity, const char* msg) noexcept override {
        // Only print warnings and errors
        if (severity <= Severity::kWARNING) {
            std::cerr << "[TensorRT] " << msg << std::endl;
        }
    }
} gLogger;

// TensorRT engine structure
typedef struct {
    nvinfer1::IRuntime* runtime;
    nvinfer1::ICudaEngine* engine;
    nvinfer1::IExecutionContext* context;
    cudaStream_t stream;
    int numBindings;
} TRTEngine;

extern "C" {

// Load TensorRT engine from file
TRTEngine* trt_load_engine(const char* engine_path) {
    if (!engine_path) {
        std::cerr << "[TensorRT] Engine path is NULL" << std::endl;
        return nullptr;
    }

    // Read engine file
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

    // Create TensorRT runtime
    nvinfer1::IRuntime* runtime = nvinfer1::createInferRuntime(gLogger);
    if (!runtime) {
        std::cerr << "[TensorRT] Failed to create runtime" << std::endl;
        return nullptr;
    }

    // Deserialize engine
    nvinfer1::ICudaEngine* engine = runtime->deserializeCudaEngine(
        engineData.data(), size);
    
    if (!engine) {
        std::cerr << "[TensorRT] Failed to deserialize engine" << std::endl;
        runtime->destroy();
        return nullptr;
    }

    // Create execution context
    nvinfer1::IExecutionContext* context = engine->createExecutionContext();
    if (!context) {
        std::cerr << "[TensorRT] Failed to create execution context" << std::endl;
        engine->destroy();
        runtime->destroy();
        return nullptr;
    }

    // Create CUDA stream
    cudaStream_t stream;
    if (cudaStreamCreate(&stream) != cudaSuccess) {
        std::cerr << "[TensorRT] Failed to create CUDA stream" << std::endl;
        context->destroy();
        engine->destroy();
        runtime->destroy();
        return nullptr;
    }

    // Allocate and populate TRTEngine struct
    TRTEngine* trt = new TRTEngine();
    trt->runtime = runtime;
    trt->engine = engine;
    trt->context = context;
    trt->stream = stream;
    trt->numBindings = engine->getNbBindings();

    std::cout << "[TensorRT] Engine loaded successfully: " << engine_path << std::endl;
    std::cout << "[TensorRT] Number of bindings: " << trt->numBindings << std::endl;

    return trt;
}

// Execute TensorRT inference
int trt_execute_inference(TRTEngine* engine, void** bindings) {
    if (!engine || !engine->context) {
        std::cerr << "[TensorRT] Invalid engine or context" << std::endl;
        return -1;
    }

    // Execute inference asynchronously
    bool success = engine->context->enqueueV2(bindings, engine->stream, nullptr);
    
    if (!success) {
        std::cerr << "[TensorRT] Inference execution failed" << std::endl;
        return -1;
    }

    // Wait for completion
    cudaError_t err = cudaStreamSynchronize(engine->stream);
    if (err != cudaSuccess) {
        std::cerr << "[TensorRT] Stream synchronization failed: " 
                  << cudaGetErrorString(err) << std::endl;
        return -1;
    }

    return 0;
}

// Free TensorRT engine resources
void trt_free_engine(TRTEngine* engine) {
    if (!engine) {
        return;
    }

    if (engine->stream) {
        cudaStreamDestroy(engine->stream);
    }
    if (engine->context) {
        engine->context->destroy();
    }
    if (engine->engine) {
        engine->engine->destroy();
    }
    if (engine->runtime) {
        engine->runtime->destroy();
    }

    delete engine;
    
    std::cout << "[TensorRT] Engine resources freed" << std::endl;
}

} // extern "C"
