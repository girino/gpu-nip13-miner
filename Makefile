.PHONY: build run clean

build:
	CGO_CFLAGS="-DCL_TARGET_OPENCL_VERSION=200 -DCL_DEPTH_STENCIL=0x10FF -DCL_UNORM_INT24=0x10DF" go build -o gpu-nostr-pow

run: build
	./gpu-nostr-pow

clean:
	rm -f gpu-nostr-pow

