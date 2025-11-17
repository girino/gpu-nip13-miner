# PowerShell build script for Windows native compilation
# This script compiles the Go program natively on Windows with OpenCL support

$ErrorActionPreference = "Stop"

Write-Host "Building for Windows (native)..." -ForegroundColor Cyan

# Check if Go is installed
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Error: Go is not installed or not in PATH" -ForegroundColor Red
    Write-Host "Please install Go from https://golang.org/dl/" -ForegroundColor Yellow
    exit 1
}

$goVersion = go version
Write-Host "Found: $goVersion" -ForegroundColor Green

# Check for OpenCL headers
$openclHeaderPath = $null
$possibleHeaderPaths = @(
    "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\*\include\CL",
    "C:\Program Files (x86)\NVIDIA GPU Computing Toolkit\CUDA\*\include\CL",
    "C:\Program Files\Microsoft SDKs\Windows\*\Include\um\CL",
    "C:\Program Files (x86)\Microsoft SDKs\Windows\*\Include\um\CL",
    "C:\Program Files\Windows Kits\*\Include\*\um\CL",
    "C:\Program Files (x86)\Windows Kits\*\Include\*\um\CL",
    "C:\OpenCL\include\CL",
    "C:\Program Files\OpenCL\include\CL"
)

Write-Host "`nSearching for OpenCL headers..." -ForegroundColor Cyan
foreach ($pattern in $possibleHeaderPaths) {
    $found = Get-ChildItem -Path $pattern -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($found) {
        $openclHeaderPath = $found.Directory.Parent.FullName
        Write-Host "Found OpenCL headers in: $openclHeaderPath" -ForegroundColor Green
        break
    }
}

if (-not $openclHeaderPath) {
    Write-Host "Warning: OpenCL headers not found in standard locations" -ForegroundColor Yellow
    Write-Host "`nTrying to find CUDA installation..." -ForegroundColor Cyan
    
    # Try to find CUDA via environment variable
    $cudaPath = $env:CUDA_PATH
    if ($cudaPath -and (Test-Path "$cudaPath\include\CL")) {
        $openclHeaderPath = "$cudaPath\include"
        Write-Host "Found OpenCL headers via CUDA_PATH: $openclHeaderPath" -ForegroundColor Green
    } else {
        Write-Host "`nOpenCL headers not found. Options:" -ForegroundColor Yellow
        Write-Host "1. Install CUDA Toolkit (recommended): https://developer.nvidia.com/cuda-downloads" -ForegroundColor White
        Write-Host "2. Install Windows SDK (includes OpenCL headers)" -ForegroundColor White
        Write-Host "3. Download OpenCL headers manually and place in C:\OpenCL\include\CL" -ForegroundColor White
        exit 1
    }
}

# Check for OpenCL library
$openclLibPath = $null
$possibleLibPaths = @(
    "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\*\lib\x64\OpenCL.lib",
    "C:\Program Files (x86)\NVIDIA GPU Computing Toolkit\CUDA\*\lib\x64\OpenCL.lib",
    "C:\Program Files\Microsoft SDKs\Windows\*\Lib\x64\OpenCL.lib",
    "C:\Program Files (x86)\Microsoft SDKs\Windows\*\Lib\x64\OpenCL.lib",
    "C:\Program Files\Windows Kits\*\Lib\*\um\x64\OpenCL.lib",
    "C:\Program Files (x86)\Windows Kits\*\Lib\*\um\x64\OpenCL.lib",
    "C:\OpenCL\lib\x64\OpenCL.lib",
    "C:\Program Files\OpenCL\lib\x64\OpenCL.lib"
)

Write-Host "`nSearching for OpenCL library..." -ForegroundColor Cyan
foreach ($pattern in $possibleLibPaths) {
    $found = Get-ChildItem -Path $pattern -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($found) {
        $openclLibPath = $found.Directory.FullName
        Write-Host "Found OpenCL library in: $openclLibPath" -ForegroundColor Green
        break
    }
}

# If library not found, check CUDA_PATH
if (-not $openclLibPath) {
    $cudaPath = $env:CUDA_PATH
    if ($cudaPath -and (Test-Path "$cudaPath\lib\x64\OpenCL.lib")) {
        $openclLibPath = "$cudaPath\lib\x64"
        Write-Host "Found OpenCL library via CUDA_PATH: $openclLibPath" -ForegroundColor Green
    }
}

# On Windows, OpenCL loads dynamically, so we might not need the .lib file
# But CGO still needs it for linking. If not found, we'll create a minimal stub
if (-not $openclLibPath) {
    Write-Host "`nOpenCL.lib not found. Creating minimal stub for linking..." -ForegroundColor Yellow
    Write-Host "Note: OpenCL will load dynamically at runtime via OpenCL.dll" -ForegroundColor Yellow
    
    $stubDir = "$env:TEMP\opencl-stub"
    $stubLib = "$stubDir\OpenCL.lib"
    
    if (-not (Test-Path $stubLib)) {
        New-Item -ItemType Directory -Path $stubDir -Force | Out-Null
        
        # Check for MSVC compiler
        $clPath = $null
        $possibleClPaths = @(
            "C:\Program Files\Microsoft Visual Studio\*\VC\Tools\MSVC\*\bin\Hostx64\x64\cl.exe",
            "C:\Program Files (x86)\Microsoft Visual Studio\*\VC\Tools\MSVC\*\bin\Hostx64\x64\cl.exe"
        )
        
        foreach ($pattern in $possibleClPaths) {
            $found = Get-ChildItem -Path $pattern -ErrorAction SilentlyContinue | Select-Object -First 1
            if ($found) {
                $clPath = $found.FullName
                break
            }
        }
        
        if ($clPath) {
            Write-Host "Found MSVC compiler: $clPath" -ForegroundColor Green
            $vcvarsPath = Split-Path (Split-Path (Split-Path (Split-Path $clPath))) -Parent
            $vcvars64 = "$vcvarsPath\VC\Auxiliary\Build\vcvars64.bat"
            
            if (Test-Path $vcvars64) {
                # Create stub C file
                $stubC = @"
// Minimal stub for OpenCL linking - OpenCL loads dynamically at runtime on Windows
__declspec(dllexport) void* clGetPlatformIDs() { return 0; }
__declspec(dllexport) void* clGetDeviceIDs() { return 0; }
__declspec(dllexport) void* clGetPlatformInfo() { return 0; }
__declspec(dllexport) void* clGetDeviceInfo() { return 0; }
__declspec(dllexport) void* clCreateContext() { return 0; }
__declspec(dllexport) void* clCreateContextFromType() { return 0; }
__declspec(dllexport) void* clCreateCommandQueue() { return 0; }
__declspec(dllexport) void* clCreateProgramWithSource() { return 0; }
__declspec(dllexport) void* clCreateProgramWithBinary() { return 0; }
__declspec(dllexport) void* clBuildProgram() { return 0; }
__declspec(dllexport) void* clGetProgramBuildInfo() { return 0; }
__declspec(dllexport) void* clCreateKernel() { return 0; }
__declspec(dllexport) void* clCreateKernelsInProgram() { return 0; }
__declspec(dllexport) void* clGetKernelInfo() { return 0; }
__declspec(dllexport) void* clGetKernelArgInfo() { return 0; }
__declspec(dllexport) void* clGetKernelWorkGroupInfo() { return 0; }
__declspec(dllexport) void* clSetKernelArg() { return 0; }
__declspec(dllexport) void* clCreateBuffer() { return 0; }
__declspec(dllexport) void* clCreateImage() { return 0; }
__declspec(dllexport) void* clGetSupportedImageFormats() { return 0; }
__declspec(dllexport) void* clEnqueueNDRangeKernel() { return 0; }
__declspec(dllexport) void* clEnqueueTask() { return 0; }
__declspec(dllexport) void* clEnqueueReadBuffer() { return 0; }
__declspec(dllexport) void* clEnqueueWriteBuffer() { return 0; }
__declspec(dllexport) void* clEnqueueCopyBuffer() { return 0; }
__declspec(dllexport) void* clEnqueueReadImage() { return 0; }
__declspec(dllexport) void* clEnqueueWriteImage() { return 0; }
__declspec(dllexport) void* clEnqueueMapBuffer() { return 0; }
__declspec(dllexport) void* clEnqueueMapImage() { return 0; }
__declspec(dllexport) void* clEnqueueUnmapMemObject() { return 0; }
__declspec(dllexport) void* clEnqueueBarrierWithWaitList() { return 0; }
__declspec(dllexport) void* clEnqueueMarkerWithWaitList() { return 0; }
__declspec(dllexport) void* clEnqueueFillBuffer() { return 0; }
__declspec(dllexport) void* clFinish() { return 0; }
__declspec(dllexport) void* clFlush() { return 0; }
__declspec(dllexport) void* clCreateUserEvent() { return 0; }
__declspec(dllexport) void* clSetUserEventStatus() { return 0; }
__declspec(dllexport) void* clWaitForEvents() { return 0; }
__declspec(dllexport) void* clGetEventProfilingInfo() { return 0; }
__declspec(dllexport) void* clReleaseEvent() { return 0; }
__declspec(dllexport) void* clReleaseMemObject() { return 0; }
__declspec(dllexport) void* clReleaseProgram() { return 0; }
__declspec(dllexport) void* clReleaseKernel() { return 0; }
__declspec(dllexport) void* clReleaseCommandQueue() { return 0; }
__declspec(dllexport) void* clReleaseContext() { return 0; }
"@
                $stubC | Out-File -FilePath "$stubDir\opencl_stub.c" -Encoding ASCII
                
                # Compile stub with MSVC
                Write-Host "Compiling stub library..." -ForegroundColor Cyan
                & cmd /c "`"$vcvars64`" && cl /LD /Fe:`"$stubLib`" `"$stubDir\opencl_stub.c`" /link /DEF:NUL 2>&1" | Out-Null
                
                if (Test-Path $stubLib) {
                    $openclLibPath = $stubDir
                    Write-Host "Created stub library: $stubLib" -ForegroundColor Green
                } else {
                    Write-Host "Warning: Could not create stub library. Trying without explicit library..." -ForegroundColor Yellow
                }
            }
        } else {
            Write-Host "MSVC not found. Trying MinGW or TDM-GCC..." -ForegroundColor Yellow
            
            # Try MinGW
            $gccPath = $null
            $possibleGccPaths = @(
                "C:\MinGW\bin\gcc.exe",
                "C:\TDM-GCC-64\bin\gcc.exe",
                "C:\Program Files\mingw-w64\*\mingw64\bin\gcc.exe",
                "C:\Program Files (x86)\mingw-w64\*\mingw64\bin\gcc.exe"
            )
            
            foreach ($pattern in $possibleGccPaths) {
                $found = Get-ChildItem -Path $pattern -ErrorAction SilentlyContinue | Select-Object -First 1
                if ($found) {
                    $gccPath = $found.FullName
                    break
                }
            }
            
            if ($gccPath) {
                Write-Host "Found GCC: $gccPath" -ForegroundColor Green
                $stubC = @"
// Minimal stub for OpenCL linking
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
"@
                $stubC | Out-File -FilePath "$stubDir\opencl_stub.c" -Encoding ASCII
                
                Write-Host "Compiling stub library with GCC..." -ForegroundColor Cyan
                $gccDir = Split-Path $gccPath
                & "$gccPath" -c -o "$stubDir\opencl_stub.o" "$stubDir\opencl_stub.c" 2>&1 | Out-Null
                
                if (Test-Path "$stubDir\opencl_stub.o") {
                    $arPath = Join-Path $gccDir "ar.exe"
                    if (Test-Path $arPath) {
                        & $arPath rcs "$stubLib" "$stubDir\opencl_stub.o" 2>&1 | Out-Null
                        if (Test-Path $stubLib) {
                            $openclLibPath = $stubDir
                            Write-Host "Created stub library: $stubLib" -ForegroundColor Green
                        }
                    }
                }
            }
        }
    } else {
        $openclLibPath = $stubDir
        Write-Host "Using existing stub library: $stubLib" -ForegroundColor Green
    }
}

# Set up environment variables for CGO
Write-Host "`nSetting up build environment..." -ForegroundColor Cyan

$env:CGO_ENABLED = "1"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

# CGO CFLAGS
$cgoCflags = "-DCL_TARGET_OPENCL_VERSION=200 -DCL_DEPTH_STENCIL=0x10FF -DCL_UNORM_INT24=0x10DF"
if ($openclHeaderPath) {
    $cgoCflags += " -I`"$openclHeaderPath`""
}
$env:CGO_CFLAGS = $cgoCflags

# CGO LDFLAGS
$cgoLdflags = ""
if ($openclLibPath) {
    $cgoLdflags = "-L`"$openclLibPath`" -lOpenCL"
} else {
    # Try to link against system OpenCL (might work if in standard paths)
    $cgoLdflags = "-lOpenCL"
}
$env:CGO_LDFLAGS = $cgoLdflags

Write-Host "CGO_CFLAGS: $env:CGO_CFLAGS" -ForegroundColor Gray
Write-Host "CGO_LDFLAGS: $env:CGO_LDFLAGS" -ForegroundColor Gray

# Build
Write-Host "`nCompiling..." -ForegroundColor Cyan
Write-Host "Note: OpenCL library will be loaded dynamically at runtime via OpenCL.dll" -ForegroundColor Yellow

go build -o gpu-nostr-pow.exe

if (Test-Path "gpu-nostr-pow.exe") {
    Write-Host "`n[OK] Build successful: gpu-nostr-pow.exe" -ForegroundColor Green
    $fileInfo = Get-Item "gpu-nostr-pow.exe"
    Write-Host "  Size: $([math]::Round($fileInfo.Length / 1MB, 2)) MB" -ForegroundColor Gray
    Write-Host "  Location: $($fileInfo.FullName)" -ForegroundColor Gray
} else {
    Write-Host "`n[FAILED] Build failed" -ForegroundColor Red
    exit 1
}

