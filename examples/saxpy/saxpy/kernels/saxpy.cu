#include <cuda_runtime.h>

#include "saxpy.h"

/* CUDA-KRN-002: every kernel guards element access with an explicit
 * index bound check. */
__global__ void saxpy_kernel(int n, float a, const float *x, float *y) {
    int i = blockIdx.x * blockDim.x + threadIdx.x;
    if (i < n) {
        y[i] = a * x[i] + y[i];
    }
}

extern "C" int launch_saxpy(int n, float a, const float *x, float *y) {
    float *dx = 0, *dy = 0;
    size_t bytes = (size_t)n * sizeof(float);
    cudaError_t err;

    if ((err = cudaMalloc(&dx, bytes)) != cudaSuccess) goto out;
    if ((err = cudaMalloc(&dy, bytes)) != cudaSuccess) goto out;
    if ((err = cudaMemcpy(dx, x, bytes, cudaMemcpyHostToDevice)) != cudaSuccess) goto out;
    if ((err = cudaMemcpy(dy, y, bytes, cudaMemcpyHostToDevice)) != cudaSuccess) goto out;

    {
        int block = 256;
        int grid = (n + block - 1) / block;
        saxpy_kernel<<<grid, block>>>(n, a, dx, dy);
        /* CUDA-KRN-001: check cudaGetLastError after every launch and
         * propagate the numeric value to the caller. */
        if ((err = cudaGetLastError()) != cudaSuccess) goto out;
    }

    err = cudaMemcpy(y, dy, bytes, cudaMemcpyDeviceToHost);

out:
    if (dx) cudaFree(dx);
    if (dy) cudaFree(dy);
    return (int)err;
}
