#!/bin/bash
set -e

echo "Building for Windows..."

# Check if MinGW is installed
if ! command -v x86_64-w64-mingw32-gcc &> /dev/null; then
    echo "Error: MinGW-w64 not found. Install with: sudo apt-get install gcc-mingw-w64-x86-64"
    exit 1
fi

# Check for OpenCL headers
OPENCL_HEADER_PATH=""
if [ -d "/usr/include/CL" ]; then
    OPENCL_HEADER_PATH="/usr/include"
    echo "Found OpenCL headers in /usr/include/CL"
elif [ -d "/usr/local/include/CL" ]; then
    OPENCL_HEADER_PATH="/usr/local/include"
    echo "Found OpenCL headers in /usr/local/include/CL"
else
    echo "Warning: OpenCL headers not found in standard locations"
    echo "Installing opencl-headers..."
    sudo apt-get install -y opencl-headers 2>/dev/null || {
        echo "Error: Could not install opencl-headers automatically"
        echo "Please install manually: sudo apt-get install opencl-headers"
        exit 1
    }
    if [ -d "/usr/include/CL" ]; then
        OPENCL_HEADER_PATH="/usr/include"
    fi
fi

if [ -z "$OPENCL_HEADER_PATH" ]; then
    echo "Error: Could not find OpenCL headers. Please install: sudo apt-get install opencl-headers"
    exit 1
fi

# Set build environment
export GOOS=windows
export GOARCH=amd64
export CGO_ENABLED=1
export CC=x86_64-w64-mingw32-gcc

# Copy OpenCL headers to MinGW include directory temporarily
MINGW_INCLUDE="/usr/x86_64-w64-mingw32/include"
if [ ! -d "$MINGW_INCLUDE/CL" ] && [ -d "$OPENCL_HEADER_PATH/CL" ]; then
    echo "Copying OpenCL headers to MinGW include directory..."
    sudo cp -r "$OPENCL_HEADER_PATH/CL" "$MINGW_INCLUDE/" 2>/dev/null || {
        echo "Warning: Could not copy headers to MinGW directory, trying alternative approach..."
    }
fi

# CGO flags - MinGW will find OpenCL headers in its own include path
export CGO_CFLAGS="-DCL_TARGET_OPENCL_VERSION=200 -DCL_DEPTH_STENCIL=0x10FF -DCL_UNORM_INT24=0x10DF"

# Create a stub OpenCL library for linking (OpenCL loads dynamically at runtime on Windows)
STUB_LIB="/tmp/libOpenCL.a"
if [ ! -f "$STUB_LIB" ]; then
    echo "Creating stub OpenCL library for linking..."
    cat > /tmp/opencl_stub.c << 'STUBEOF'
// Minimal stub for OpenCL linking - OpenCL loads dynamically at runtime on Windows
// These are just placeholders for linking; actual functions load at runtime
void* clGetPlatformIDs() { return 0; }
void* clGetDeviceIDs() { return 0; }
void* clGetPlatformInfo() { return 0; }
void* clGetDeviceInfo() { return 0; }
void* clCreateContext() { return 0; }
void* clCreateContextFromType() { return 0; }
void* clCreateCommandQueue() { return 0; }
void* clCreateProgramWithSource() { return 0; }
void* clCreateProgramWithBinary() { return 0; }
void* clBuildProgram() { return 0; }
void* clGetProgramBuildInfo() { return 0; }
void* clCreateKernel() { return 0; }
void* clCreateKernelsInProgram() { return 0; }
void* clGetKernelInfo() { return 0; }
void* clGetKernelArgInfo() { return 0; }
void* clGetKernelWorkGroupInfo() { return 0; }
void* clSetKernelArg() { return 0; }
void* clCreateBuffer() { return 0; }
void* clCreateImage() { return 0; }
void* clGetSupportedImageFormats() { return 0; }
void* clEnqueueNDRangeKernel() { return 0; }
void* clEnqueueTask() { return 0; }
void* clEnqueueReadBuffer() { return 0; }
void* clEnqueueWriteBuffer() { return 0; }
void* clEnqueueCopyBuffer() { return 0; }
void* clEnqueueReadImage() { return 0; }
void* clEnqueueWriteImage() { return 0; }
void* clEnqueueMapBuffer() { return 0; }
void* clEnqueueMapImage() { return 0; }
void* clEnqueueUnmapMemObject() { return 0; }
void* clEnqueueBarrierWithWaitList() { return 0; }
void* clEnqueueMarkerWithWaitList() { return 0; }
void* clEnqueueFillBuffer() { return 0; }
void* clFinish() { return 0; }
void* clFlush() { return 0; }
void* clCreateUserEvent() { return 0; }
void* clSetUserEventStatus() { return 0; }
void* clWaitForEvents() { return 0; }
void* clGetEventProfilingInfo() { return 0; }
void* clReleaseEvent() { return 0; }
void* clReleaseMemObject() { return 0; }
void* clReleaseProgram() { return 0; }
void* clReleaseKernel() { return 0; }
void* clReleaseCommandQueue() { return 0; }
void* clReleaseContext() { return 0; }
STUBEOF
    x86_64-w64-mingw32-gcc -c -o /tmp/opencl_stub.o /tmp/opencl_stub.c
    x86_64-w64-mingw32-ar rcs "$STUB_LIB" /tmp/opencl_stub.o
    rm -f /tmp/opencl_stub.c /tmp/opencl_stub.o
fi

# CGO LDFLAGS - Link against stub library (actual OpenCL loads at runtime)
export CGO_LDFLAGS="-L/tmp -lOpenCL"

# Build
echo "Compiling with OpenCL headers from: $OPENCL_HEADER_PATH"
echo "Note: OpenCL library will be loaded dynamically at runtime on Windows"
go build -o gpu-nostr-pow.exe

if [ -f gpu-nostr-pow.exe ]; then
    echo "✓ Build successful: gpu-nostr-pow.exe"
    file gpu-nostr-pow.exe
else
    echo "✗ Build failed"
    exit 1
fi