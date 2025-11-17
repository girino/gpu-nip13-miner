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

# Function to download OpenCL headers automatically
function Download-OpenCLHeaders {
    $openclDir = "C:\OpenCL\include\CL"
    $openclIncludeDir = "C:\OpenCL\include"
    
    # Check if directory exists AND contains headers
    if (Test-Path $openclDir) {
        $clHeaderFile = Join-Path $openclDir "cl.h"
        if (Test-Path $clHeaderFile) {
            Write-Host "OpenCL headers directory already exists with headers: $openclDir" -ForegroundColor Green
            return $openclIncludeDir
        } else {
            Write-Host "OpenCL headers directory exists but is empty or missing headers" -ForegroundColor Yellow
            Write-Host "Removing empty directory and re-downloading..." -ForegroundColor Gray
            Remove-Item -Path $openclDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
    
    # Try git first, then fall back to ZIP download
    $gitSuccess = $false
    
    # Check if git is available
    if (Get-Command git -ErrorAction SilentlyContinue) {
        Write-Host "`nDownloading OpenCL headers from Khronos Group using git..." -ForegroundColor Cyan
        
        # Create temporary directory for cloning
        $tempDir = Join-Path $env:TEMP "opencl-headers-temp"
        if (Test-Path $tempDir) {
            Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
        }
        
        try {
            # Clone the repository (shallow clone)
            Write-Host "  Cloning OpenCL-Headers repository..." -ForegroundColor Gray
            $cloneOutput = & git clone --depth 1 https://github.com/KhronosGroup/OpenCL-Headers.git $tempDir 2>&1 | Out-String
            $exitCode = $LASTEXITCODE
            
            # Check if clone was successful by verifying the directory was created
            if ($exitCode -eq 0 -and (Test-Path $tempDir)) {
                Write-Host "  Repository cloned successfully" -ForegroundColor Green
                
                # Create target directory
                New-Item -ItemType Directory -Path $openclDir -Force | Out-Null
                
                # Copy CL directory contents
                $sourceCL = Join-Path $tempDir "CL"
                if (Test-Path $sourceCL) {
                    Write-Host "  Copying headers from repository..." -ForegroundColor Gray
                    Copy-Item -Path "$sourceCL\*" -Destination $openclDir -Recurse -Force
                    $headerCount = (Get-ChildItem -Path $openclDir -File).Count
                    if ($headerCount -gt 0) {
                        Write-Host "  Copied $headerCount header files to $openclDir" -ForegroundColor Green
                        $gitSuccess = $true
                    }
                }
            }
        } catch {
            Write-Host "  Git clone failed: $($_.Exception.Message)" -ForegroundColor Yellow
        } finally {
            # Clean up temporary directory
            if (Test-Path $tempDir) {
                Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
    } else {
        Write-Host "`nGit not available, will try ZIP download instead..." -ForegroundColor Yellow
    }
    
    # If git failed or not available, try ZIP download
    if (-not $gitSuccess) {
        Write-Host "`nDownloading OpenCL headers from GitHub ZIP..." -ForegroundColor Cyan
        
        # Enable TLS 1.2 for older PowerShell versions
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        
        $zipUrl = "https://github.com/KhronosGroup/OpenCL-Headers/archive/refs/heads/main.zip"
        $zipFile = Join-Path $env:TEMP "opencl-headers.zip"
        $extractDir = Join-Path $env:TEMP "opencl-headers-extract"
        
        try {
            # Download ZIP file
            Write-Host "  Downloading ZIP from GitHub..." -ForegroundColor Gray
            Invoke-WebRequest -Uri $zipUrl -OutFile $zipFile -UseBasicParsing -ErrorAction Stop
            
            # Extract ZIP file
            Write-Host "  Extracting ZIP file..." -ForegroundColor Gray
            if (Test-Path $extractDir) {
                Remove-Item -Path $extractDir -Recurse -Force -ErrorAction SilentlyContinue
            }
            Expand-Archive -Path $zipFile -DestinationPath $extractDir -Force
            
            # Find the CL directory in the extracted files
            $extractedCL = Join-Path $extractDir "OpenCL-Headers-main\CL"
            if (-not (Test-Path $extractedCL)) {
                # Try alternative path structure
                $extractedCL = Get-ChildItem -Path $extractDir -Recurse -Directory -Filter "CL" -ErrorAction SilentlyContinue | Select-Object -First 1
                if ($extractedCL) {
                    $extractedCL = $extractedCL.FullName
                }
            }
            
            if (Test-Path $extractedCL) {
                # Create target directory
                New-Item -ItemType Directory -Path $openclDir -Force | Out-Null
                
                # Copy CL directory contents
                Write-Host "  Copying headers from extracted ZIP..." -ForegroundColor Gray
                Copy-Item -Path "$extractedCL\*" -Destination $openclDir -Recurse -Force
                $headerCount = (Get-ChildItem -Path $openclDir -File).Count
                if ($headerCount -gt 0) {
                    Write-Host "  Copied $headerCount header files to $openclDir" -ForegroundColor Green
                    return $openclIncludeDir
                } else {
                    Write-Host "  Error: No header files were copied from ZIP" -ForegroundColor Red
                    return $null
                }
            } else {
                Write-Host "  Error: CL directory not found in extracted ZIP" -ForegroundColor Red
                return $null
            }
        } catch {
            Write-Host "  Error: Failed to download or extract ZIP - $($_.Exception.Message)" -ForegroundColor Red
            return $null
        } finally {
            # Clean up temporary files
            if (Test-Path $zipFile) {
                Remove-Item -Path $zipFile -Force -ErrorAction SilentlyContinue
            }
            if (Test-Path $extractDir) {
                Remove-Item -Path $extractDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
    } else {
        return $openclIncludeDir
    }
}

# Check for OpenCL headers
$openclHeaderPath = $null
$possibleHeaderPaths = @(
    # Intel OpenCL SDK
    "C:\Program Files (x86)\Intel\OpenCL SDK\*\include\CL",
    "C:\Program Files\Intel\OpenCL SDK\*\include\CL",
    "C:\Program Files (x86)\IntelSWTools\OpenCL\sdk\include\CL",
    "C:\Program Files\IntelSWTools\OpenCL\sdk\include\CL",
    # NVIDIA CUDA (for completeness, but not required)
    "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\*\include\CL",
    "C:\Program Files (x86)\NVIDIA GPU Computing Toolkit\CUDA\*\include\CL",
    # Windows SDK
    "C:\Program Files\Microsoft SDKs\Windows\*\Include\um\CL",
    "C:\Program Files (x86)\Microsoft SDKs\Windows\*\Include\um\CL",
    "C:\Program Files\Windows Kits\*\Include\*\um\CL",
    "C:\Program Files (x86)\Windows Kits\*\Include\*\um\CL",
    # Generic OpenCL installations
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
    Write-Host "`nTrying environment variables..." -ForegroundColor Cyan
    
    # Try Intel OpenCL SDK environment variable
    $intelOCLPath = $env:INTELOCLSDKROOT
    if ($intelOCLPath -and (Test-Path "$intelOCLPath\include\CL")) {
        $openclHeaderPath = "$intelOCLPath\include"
        Write-Host "Found OpenCL headers via INTELOCLSDKROOT: $openclHeaderPath" -ForegroundColor Green
    }
    
    # Try CUDA via environment variable (for NVIDIA systems)
    if (-not $openclHeaderPath) {
        $cudaPath = $env:CUDA_PATH
        if ($cudaPath -and (Test-Path "$cudaPath\include\CL")) {
            $openclHeaderPath = "$cudaPath\include"
            Write-Host "Found OpenCL headers via CUDA_PATH: $openclHeaderPath" -ForegroundColor Green
        }
    }
    
    if (-not $openclHeaderPath) {
        Write-Host "`nOpenCL headers not found." -ForegroundColor Yellow
        Write-Host "Attempting to download OpenCL headers automatically..." -ForegroundColor Cyan
        
        $downloadedPath = Download-OpenCLHeaders
        if ($downloadedPath -and (Test-Path "$downloadedPath\CL")) {
            $openclHeaderPath = $downloadedPath
            Write-Host "Successfully set up OpenCL headers in: $openclHeaderPath" -ForegroundColor Green
        } else {
            Write-Host "`nAutomatic download failed. Manual options:" -ForegroundColor Yellow
            Write-Host "`nQUICK FIX - Download OpenCL headers manually:" -ForegroundColor Cyan
            Write-Host "1. Download ZIP from: https://github.com/KhronosGroup/OpenCL-Headers/archive/refs/heads/main.zip" -ForegroundColor White
            Write-Host "2. Extract the ZIP file" -ForegroundColor White
            Write-Host "3. Copy the 'CL' folder contents to: C:\OpenCL\include\CL" -ForegroundColor White
            Write-Host "   (The folder structure should be: C:\OpenCL\include\CL\cl.h)" -ForegroundColor Gray
            Write-Host "   Note: Headers are directly in the CL/ directory, not in opencl22/CL/" -ForegroundColor Gray
            Write-Host "`nALTERNATIVE OPTIONS:" -ForegroundColor Cyan
            Write-Host "1. Install Intel OpenCL SDK: https://www.intel.com/content/www/us/en/developer/tools/opencl-sdk/overview.html" -ForegroundColor White
            Write-Host "2. Install Windows SDK (includes OpenCL headers):" -ForegroundColor White
            Write-Host "   - Download from: https://developer.microsoft.com/en-us/windows/downloads/windows-sdk/" -ForegroundColor Gray
            Write-Host "   - Or via Visual Studio Installer: Modify -> Individual components -> Windows SDK" -ForegroundColor Gray
            Write-Host "`nTo check if Windows SDK is installed, run:" -ForegroundColor Cyan
            Write-Host "  Get-ChildItem 'C:\Program Files\Windows Kits\*\Include\*\um\CL' -ErrorAction SilentlyContinue" -ForegroundColor Gray
            exit 1
        }
    }
}

# Check for OpenCL library
$openclLibPath = $null
$possibleLibPaths = @(
    # Intel OpenCL SDK
    "C:\Program Files (x86)\Intel\OpenCL SDK\*\lib\x64\OpenCL.lib",
    "C:\Program Files\Intel\OpenCL SDK\*\lib\x64\OpenCL.lib",
    "C:\Program Files (x86)\IntelSWTools\OpenCL\sdk\lib\x64\OpenCL.lib",
    "C:\Program Files\IntelSWTools\OpenCL\sdk\lib\x64\OpenCL.lib",
    # NVIDIA CUDA (for completeness)
    "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\*\lib\x64\OpenCL.lib",
    "C:\Program Files (x86)\NVIDIA GPU Computing Toolkit\CUDA\*\lib\x64\OpenCL.lib",
    # Windows SDK
    "C:\Program Files\Microsoft SDKs\Windows\*\Lib\x64\OpenCL.lib",
    "C:\Program Files (x86)\Microsoft SDKs\Windows\*\Lib\x64\OpenCL.lib",
    "C:\Program Files\Windows Kits\*\Lib\*\um\x64\OpenCL.lib",
    "C:\Program Files (x86)\Windows Kits\*\Lib\*\um\x64\OpenCL.lib",
    # Generic OpenCL installations
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

# If library not found, check environment variables
if (-not $openclLibPath) {
    # Try Intel OpenCL SDK
    $intelOCLPath = $env:INTELOCLSDKROOT
    if ($intelOCLPath -and (Test-Path "$intelOCLPath\lib\x64\OpenCL.lib")) {
        $openclLibPath = "$intelOCLPath\lib\x64"
        Write-Host "Found OpenCL library via INTELOCLSDKROOT: $openclLibPath" -ForegroundColor Green
    }
    
    # Try CUDA_PATH (for NVIDIA systems)
    if (-not $openclLibPath) {
        $cudaPath = $env:CUDA_PATH
        if ($cudaPath -and (Test-Path "$cudaPath\lib\x64\OpenCL.lib")) {
            $openclLibPath = "$cudaPath\lib\x64"
            Write-Host "Found OpenCL library via CUDA_PATH: $openclLibPath" -ForegroundColor Green
        }
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

# Detect C compiler (prefer MSYS2 MinGW64 GCC)
Write-Host "`nDetecting C compiler..." -ForegroundColor Cyan
$cCompiler = $null
$cCompilerPath = $null

# Check if gcc is already in PATH
if (Get-Command gcc -ErrorAction SilentlyContinue) {
    $gccPath = (Get-Command gcc).Source
    Write-Host "Found GCC in PATH: $gccPath" -ForegroundColor Green
    $cCompiler = "gcc"
    $cCompilerPath = $gccPath
} else {
    # Try to find MSYS2 MinGW64 GCC
    $msys2Paths = @(
        "C:\msys64\mingw64\bin\gcc.exe",
        "C:\msys64\ucrt64\bin\gcc.exe",
        "C:\msys64\clang64\bin\gcc.exe",
        "$env:USERPROFILE\msys64\mingw64\bin\gcc.exe",
        "$env:USERPROFILE\msys64\ucrt64\bin\gcc.exe"
    )
    
    foreach ($path in $msys2Paths) {
        if (Test-Path $path) {
            $cCompilerPath = $path
            Write-Host "Found MSYS2 MinGW64 GCC: $path" -ForegroundColor Green
            
            # Add MSYS2 bin directory to PATH for this session
            $msys2Bin = Split-Path $path
            if ($env:PATH -notlike "*$msys2Bin*") {
                $env:PATH = "$msys2Bin;$env:PATH"
                Write-Host "Added MSYS2 bin to PATH: $msys2Bin" -ForegroundColor Gray
            }
            # Use just "gcc" since it's now in PATH
            $cCompiler = "gcc"
            break
        }
    }
    
    # Try other common GCC installations
    if (-not $cCompilerPath) {
        $otherGccPaths = @(
            "C:\MinGW\bin\gcc.exe",
            "C:\TDM-GCC-64\bin\gcc.exe",
            "C:\Program Files\mingw-w64\*\mingw64\bin\gcc.exe",
            "C:\Program Files (x86)\mingw-w64\*\mingw64\bin\gcc.exe"
        )
        
        foreach ($pattern in $otherGccPaths) {
            $found = Get-ChildItem -Path $pattern -ErrorAction SilentlyContinue | Select-Object -First 1
            if ($found) {
                $cCompilerPath = $found.FullName
                Write-Host "Found GCC: $cCompilerPath" -ForegroundColor Green
                
                # Add to PATH
                $gccBin = Split-Path $cCompilerPath
                if ($env:PATH -notlike "*$gccBin*") {
                    $env:PATH = "$gccBin;$env:PATH"
                    Write-Host "Added GCC bin to PATH: $gccBin" -ForegroundColor Gray
                }
                # Use just "gcc" since it's now in PATH
                $cCompiler = "gcc"
                break
            }
        }
    }
}

if (-not $cCompilerPath) {
    Write-Host "Error: C compiler (gcc) not found!" -ForegroundColor Red
    Write-Host "`nPlease install MSYS2 MinGW64:" -ForegroundColor Yellow
    Write-Host "1. Download from: https://www.msys2.org/" -ForegroundColor White
    Write-Host "2. Install MSYS2" -ForegroundColor White
    Write-Host "3. Open MSYS2 MinGW64 terminal and run: pacman -S mingw-w64-x86_64-gcc" -ForegroundColor White
    Write-Host "4. Or add MSYS2 MinGW64 bin to your PATH:" -ForegroundColor White
    Write-Host "   C:\msys64\mingw64\bin" -ForegroundColor Gray
    exit 1
}

# Set up environment variables for CGO
Write-Host "`nSetting up build environment..." -ForegroundColor Cyan

$env:CGO_ENABLED = "1"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CC = $cCompiler

# CGO CFLAGS
$cgoCflags = "-DCL_TARGET_OPENCL_VERSION=200 -DCL_DEPTH_STENCIL=0x10FF -DCL_UNORM_INT24=0x10DF"
if ($openclHeaderPath) {
    # Verify that CL/cl.h exists at the expected location
    $clHeaderPath = Join-Path $openclHeaderPath "CL\cl.h"
    $headerFound = $false
    
    if (Test-Path $clHeaderPath) {
        Write-Host "Verified OpenCL header exists: $clHeaderPath" -ForegroundColor Green
        $headerFound = $true
    } else {
        Write-Host "Warning: OpenCL header not found at expected location: $clHeaderPath" -ForegroundColor Yellow
        Write-Host "Checking directory structure..." -ForegroundColor Gray
        
        # Check if there's a nested CL directory (common mistake)
        $nestedCL = Join-Path $openclHeaderPath "CL\CL\cl.h"
        if (Test-Path $nestedCL) {
            Write-Host "Found headers in nested CL directory: $nestedCL" -ForegroundColor Yellow
            Write-Host "Fixing path to use nested directory..." -ForegroundColor Gray
            $openclHeaderPath = Join-Path $openclHeaderPath "CL"
            $clHeaderPath = Join-Path $openclHeaderPath "cl.h"
            if (Test-Path $clHeaderPath) {
                Write-Host "Verified OpenCL header exists: $clHeaderPath" -ForegroundColor Green
                $headerFound = $true
            }
        } else {
            # List directory contents for debugging
            if (Test-Path $openclHeaderPath) {
                Write-Host "Contents of ${openclHeaderPath}:" -ForegroundColor Gray
                $items = Get-ChildItem $openclHeaderPath -ErrorAction SilentlyContinue
                if ($items) {
                    foreach ($item in $items | Select-Object -First 10) {
                        $type = if ($item.PSIsContainer) { "DIR" } else { "FILE" }
                        Write-Host "  [$type] $($item.Name)" -ForegroundColor Gray
                    }
                } else {
                    Write-Host "  (directory is empty)" -ForegroundColor Yellow
                }
                
                # Check if cl.h is directly in the include path
                $directCL = Join-Path $openclHeaderPath "cl.h"
                if (Test-Path $directCL) {
                    Write-Host "Found cl.h directly in include path (wrong structure)" -ForegroundColor Yellow
                    Write-Host "Headers should be in: $openclHeaderPath\CL\cl.h" -ForegroundColor Yellow
                }
            } else {
                Write-Host "Directory does not exist: $openclHeaderPath" -ForegroundColor Red
            }
        }
    }
    
    if (-not $headerFound) {
        Write-Host "`nERROR: OpenCL headers not found in correct location!" -ForegroundColor Red
        Write-Host "Expected structure: $openclHeaderPath\CL\cl.h" -ForegroundColor Yellow
        Write-Host "`nPlease ensure headers are in the correct location:" -ForegroundColor Yellow
        Write-Host "1. Headers should be in: C:\OpenCL\include\CL\" -ForegroundColor White
        Write-Host "2. Files should include: cl.h, cl_platform.h, cl_ext.h, etc." -ForegroundColor White
        Write-Host "3. Download from: https://github.com/KhronosGroup/OpenCL-Headers" -ForegroundColor White
        Write-Host "   Copy the CL folder contents to: C:\OpenCL\include\CL\" -ForegroundColor White
        exit 1
    }
    
    # MinGW GCC on Windows can handle Windows paths directly
    # Use Windows path with forward slashes (MinGW converts them)
    $windowsPath = $openclHeaderPath -replace '\\', '/'
    $windowsPath = $windowsPath.TrimEnd('/')
    Write-Host "Using Windows path with forward slashes: $windowsPath" -ForegroundColor Gray
    # Don't use quotes - CGO may process them incorrectly, and path has no spaces
    $cgoCflags += " -I$windowsPath"
}
$env:CGO_CFLAGS = $cgoCflags

# CGO LDFLAGS
# MinGW needs an import library (.a) to link with Windows DLLs
# Check for OpenCL.dll in System32 and create import library if needed
$openclDll = "C:\Windows\System32\OpenCL.dll"
$importLibDir = Join-Path $env:TEMP "opencl-import-lib"
$importLib = Join-Path $importLibDir "libOpenCL.a"

$cgoLdflags = ""
if ($openclLibPath) {
    # Use found OpenCL.lib path
    $cgoLdflags = "-L`"$openclLibPath`" -lOpenCL"
} elseif (Test-Path $openclDll) {
    # Create import library from OpenCL.dll if it doesn't exist
    if (-not (Test-Path $importLib)) {
        Write-Host "Creating MinGW import library from OpenCL.dll..." -ForegroundColor Cyan
        New-Item -ItemType Directory -Path $importLibDir -Force | Out-Null
        
        # Check for dlltool (MinGW tool to create import libraries)
        $dlltoolPath = $null
        if ($cCompilerPath) {
            $gccDir = Split-Path $cCompilerPath
            $dlltoolPath = Join-Path $gccDir "dlltool.exe"
            if (-not (Test-Path $dlltoolPath)) {
                # Try in bin directory
                $dlltoolPath = Join-Path (Split-Path $gccDir) "bin\dlltool.exe"
            }
        }
        
        if ($dlltoolPath -and (Test-Path $dlltoolPath)) {
            Write-Host "  Using dlltool: $dlltoolPath" -ForegroundColor Gray
            # Create .def file first (we'll create a minimal one)
            $defFile = Join-Path $importLibDir "OpenCL.def"
            $defContent = @"
EXPORTS
clGetPlatformIDs
clGetDeviceIDs
clGetPlatformInfo
clGetDeviceInfo
clCreateContext
clCreateContextFromType
clCreateCommandQueue
clCreateProgramWithSource
clCreateProgramWithBinary
clBuildProgram
clGetProgramBuildInfo
clCreateKernel
clCreateKernelsInProgram
clGetKernelInfo
clGetKernelArgInfo
clGetKernelWorkGroupInfo
clSetKernelArg
clCreateBuffer
clCreateImage
clGetSupportedImageFormats
clEnqueueNDRangeKernel
clEnqueueTask
clEnqueueReadBuffer
clEnqueueWriteBuffer
clEnqueueCopyBuffer
clEnqueueReadImage
clEnqueueWriteImage
clEnqueueMapBuffer
clEnqueueMapImage
clEnqueueUnmapMemObject
clEnqueueBarrierWithWaitList
clEnqueueMarkerWithWaitList
clEnqueueFillBuffer
clFinish
clFlush
clCreateUserEvent
clSetUserEventStatus
clWaitForEvents
clGetEventProfilingInfo
clReleaseEvent
clReleaseMemObject
clReleaseProgram
clReleaseKernel
clReleaseCommandQueue
clReleaseContext
"@
            $defContent | Out-File -FilePath $defFile -Encoding ASCII
            
            # Create import library using dlltool
            # dlltool needs just the DLL name, not the full path
            $dllName = "OpenCL.dll"
            # Get absolute paths for def and output files
            $defFileAbs = [System.IO.Path]::GetFullPath($defFile)
            $importLibAbs = [System.IO.Path]::GetFullPath($importLib)
            # Change to System32 directory so dlltool can find the DLL
            Push-Location "C:\Windows\System32"
            try {
                $dlltoolOutput = & $dlltoolPath --dllname $dllName --def $defFileAbs --output-lib $importLibAbs 2>&1
            } finally {
                Pop-Location
            }
            if ($LASTEXITCODE -eq 0 -and (Test-Path $importLibAbs)) {
                Write-Host "  Created import library: $importLibAbs" -ForegroundColor Green
                $importLib = $importLibAbs
            } else {
                Write-Host "  Warning: Failed to create import library with dlltool" -ForegroundColor Yellow
                Write-Host "  Output: $dlltoolOutput" -ForegroundColor Gray
                $importLib = $null
            }
        } else {
            Write-Host "  Warning: dlltool not found, cannot create import library" -ForegroundColor Yellow
            Write-Host "  Trying to link directly with System32 path..." -ForegroundColor Yellow
            $importLib = $null
        }
    } else {
        Write-Host "Using existing import library: $importLib" -ForegroundColor Gray
    }
    
    if ($importLib -and (Test-Path $importLib)) {
        # Use the import library
        $cgoLdflags = "-L`"$importLibDir`" -lOpenCL"
    } else {
        # Try linking directly with System32 path
        $cgoLdflags = "-L`"C:\Windows\System32`" -lOpenCL"
    }
} else {
    Write-Host "Warning: OpenCL.dll not found in System32" -ForegroundColor Yellow
    $cgoLdflags = "-lOpenCL"
}
$env:CGO_LDFLAGS = $cgoLdflags

Write-Host "CC: $env:CC" -ForegroundColor Gray
Write-Host "CGO_CFLAGS: $env:CGO_CFLAGS" -ForegroundColor Gray
Write-Host "CGO_LDFLAGS: $env:CGO_LDFLAGS" -ForegroundColor Gray

# Test if compiler can find the header
if ($openclHeaderPath) {
    Write-Host "`nTesting if compiler can find OpenCL headers..." -ForegroundColor Cyan
    $testFile = Join-Path $env:TEMP "test_opencl.c"
    $testContent = @"
#include <CL/cl.h>
int main() { return 0; }
"@
    $testContent | Out-File -FilePath $testFile -Encoding ASCII
    # Split CFLAGS properly for PowerShell
    $cflagsArray = $env:CGO_CFLAGS -split ' '
    $testOutput = & $env:CC $cflagsArray -c $testFile -o "$env:TEMP\test_opencl.o" 2>&1
    $testExitCode = $LASTEXITCODE
    Remove-Item -Path $testFile -Force -ErrorAction SilentlyContinue
    Remove-Item -Path "$env:TEMP\test_opencl.o" -Force -ErrorAction SilentlyContinue
    
    if ($testExitCode -eq 0) {
        Write-Host "  Header test passed - compiler can find OpenCL headers" -ForegroundColor Green
    } else {
        Write-Host "  Header test failed - compiler cannot find OpenCL headers" -ForegroundColor Red
        Write-Host "  Compiler output: $testOutput" -ForegroundColor Yellow
        Write-Host "  This may indicate a path issue. Trying alternative path format..." -ForegroundColor Yellow
    }
}

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

