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

# On Windows, OpenCL.dll is in System32 and loads dynamically at runtime
# If OpenCL.lib is found, use it. Otherwise, link with -lOpenCL and the linker
# will find OpenCL.dll automatically
if (-not $openclLibPath) {
    Write-Host "`nOpenCL.lib not found, but this is OK on Windows." -ForegroundColor Yellow
    Write-Host "OpenCL.dll in System32 will be used at runtime." -ForegroundColor Yellow
    Write-Host "The linker will automatically find OpenCL.dll when linking with -lOpenCL" -ForegroundColor Yellow
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
# On Windows, OpenCL.dll is in System32, so we can link directly with -lOpenCL
# If OpenCL.lib was found, include its path. Otherwise, the linker will find OpenCL.dll automatically
$cgoLdflags = ""
if ($openclLibPath) {
    $cgoLdflags = "-L`"$openclLibPath`" -lOpenCL"
} else {
    # Link against OpenCL.dll in System32 (linker will find it automatically)
    $cgoLdflags = "-lOpenCL"
}
$env:CGO_LDFLAGS = $cgoLdflags

Write-Host "CGO_CFLAGS: $env:CGO_CFLAGS" -ForegroundColor Gray
Write-Host "CGO_LDFLAGS: $env:CGO_LDFLAGS" -ForegroundColor Gray

# Build
Write-Host "`nCompiling..." -ForegroundColor Cyan
Write-Host "Note: OpenCL.dll in System32 will be used at runtime" -ForegroundColor Yellow

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

