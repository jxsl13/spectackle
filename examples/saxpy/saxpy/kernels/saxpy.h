#ifndef SAXPY_H
#define SAXPY_H

#ifdef __cplusplus
extern "C" {
#endif

/* Launches the saxpy kernel: y[i] = a*x[i] + y[i] for i in [0, n).
 * Returns 0 on success or the cudaError_t value of the first failure
 * (CUDA-KRN-001). x and y must point to n readable/writable floats. */
int launch_saxpy(int n, float a, const float *x, float *y);

#ifdef __cplusplus
}
#endif

#endif /* SAXPY_H */
