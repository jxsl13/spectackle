#import <Foundation/Foundation.h>

@interface Renderer : NSObject
@property (nonatomic, strong) id<MTLLibrary> library;
@property (nonatomic, strong) id<MTLCommandQueue> commandQueue;
@property (nonatomic, strong) id<MTLCommandBuffer> commandBuffer;
@property (nonatomic, strong) id<MTLComputeCommandEncoder> encoder;
@end

@implementation Renderer

- (void)buildPipeline {
	id fn = [library newFunctionWithName:@"add_arrays"];
	(void)fn;
}

- (void)dispatch {
	[self buildPipeline];
	[encoder dispatchThreadgroups];
}

@end
