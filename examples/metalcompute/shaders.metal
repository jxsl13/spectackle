#include <metal_stdlib>
using namespace metal;

kernel void add_arrays(device const float* a [[buffer(0)]], device float* out [[buffer(1)]], uint i [[thread_position_in_grid]]) {
	out[i] = a[i] + 1.0;
}

vertex float4 passthrough(uint vid [[vertex_id]]) {
	return float4(0);
}
