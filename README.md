# GPU NIP-13 Miner

A high-performance GPU-accelerated miner for NIP-13 proof-of-work (PoW) on Nostr. This tool uses OpenCL to leverage GPU compute power for mining Nostr events with configurable difficulty levels.

## Features

- **GPU Acceleration**: Uses OpenCL to mine on GPUs (NVIDIA, Intel, AMD) or CPUs
- **Multiple Kernel Implementations**: Choose from 6 different optimized SHA256 kernels
- **Automatic Kernel Selection**: Automatically selects the best kernel for your device
- **Dynamic Batch Sizing**: Automatically optimizes batch size based on difficulty, or configure manually
- **Device Selection**: List and select specific OpenCL devices
- **Progress Tracking**: Real-time progress bar showing nonce rate and percentage relative to expected iterations
- **Comprehensive Benchmarking**: Test all kernels and batch sizes to find optimal configuration
- **Kernel Validation**: Test all kernels to verify correctness
- **Cross-Platform**: Works on Linux, Windows, and macOS

## Kernel Implementations

The miner includes two OpenCL kernel implementations, each optimized for different hardware:

- **default**: Our original implementation, optimized for CPUs and Intel GPUs
- **ckolivas**: Adapted from sgminer's ckolivas kernel, optimized for NVIDIA and AMD GPUs

The `-kernel auto` option (default) automatically selects the best kernel based on your device:
- CPUs and Intel GPUs → `default`
- NVIDIA, AMD, and other GPUs → `ckolivas`

You can manually select a kernel using the `-kernel` flag. Use `-benchmark` to test both kernels and find the best one for your hardware.

All kernels are located in the `kernel/` directory:
- Original kernels are kept for reference (not used in compilation)
- Adapted kernels (with `-adapted` suffix) are the versions modified for NIP-13 mining

## Performance Benchmarks

Performance varies significantly based on the OpenCL device and kernel used. Use the `-benchmark` option to test all kernels and find the optimal configuration for your hardware.

### CPU Performance
**~1M nonces/s** with Intel Core i5-9500T CPU (optimal batch size: 10^4)
```
Platform 0: Portable Computing Language (The pocl project)
  [0] cpu-haswell-Intel(R) Core(TM) i5-9500T CPU @ 2.20GHz (GenuineIntel) - CPU
       Version: OpenCL 3.0 PoCL HSTR: cpu-x86_64-pc-linux-gnu-haswell
       Compute Units: 6, Work Group Size: 4096, Memory: 13850 MB
       Optimal batch size: 10^4 (10,000) - 1.09M nonces/s
       Note: Larger batch sizes may cause segmentation faults
```

### Intel GPU Performance
**~2.8M nonces/s** with Intel UHD Graphics 630 (optimal batch size: 10^6)
```
Platform 0: Intel(R) OpenCL HD Graphics (Intel(R) Corporation)
  [0] Intel(R) UHD Graphics 630 (Intel(R) Corporation) - GPU
       Version: OpenCL 3.0 NEO
       Compute Units: 24, Work Group Size: 256, Memory: 13030 MB
       Optimal batch size: 10^6 (1,000,000) - 2.81M nonces/s
```

### NVIDIA GPU Performance
**~40M nonces/s** with NVIDIA GeForce GTX 1660 Ti (optimal batch size: 10^8)
```
Platform 0: NVIDIA CUDA (NVIDIA Corporation)
  [0] NVIDIA GeForce GTX 1660 Ti (NVIDIA Corporation) - GPU
       Version: OpenCL 3.0 CUDA
       Compute Units: 24, Work Group Size: 1024, Memory: 6143 MB
       Optimal batch size: 10^8 (100,000,000) - 40.09M nonces/s
```

**Note:** Performance has been significantly improved (2x faster on NVIDIA GPUs) after optimizing the OpenCL kernel to return only the nonce index instead of the full hash result, reducing memory bandwidth by ~90%.

## Requirements

- **Go 1.21+** (for building from source)
- **OpenCL** runtime and development headers
  - **Linux**: Install `ocl-icd-opencl-dev` or vendor-specific OpenCL packages
  - **Windows**: OpenCL.dll (usually included with GPU drivers)
  - **macOS**: OpenCL framework (included by default)

## Building

### Linux

Use the provided Makefile (recommended):

```bash
make build
```

Or build directly:

```bash
make
```

The Makefile includes all necessary CGO flags for OpenCL compilation.

### Windows

Use the provided PowerShell script:

```powershell
.\build-windows.ps1
```

The script will:
- Automatically detect OpenCL headers and libraries
- Download OpenCL headers from Khronos Group if needed
- Set up the build environment for MinGW64 GCC
- Create necessary import libraries

## Usage

### Basic Usage

Mine with default settings (difficulty 16, auto-detect batch size):

```bash
./gpu-nostr-pow
```

### Specify Difficulty

```bash
./gpu-nostr-pow -difficulty 20
```

### List Available Devices

```bash
./gpu-nostr-pow -list-devices
# or
./gpu-nostr-pow -l
```

### Select Specific Device

```bash
./gpu-nostr-pow -device 0 -difficulty 16
# or
./gpu-nostr-pow -d 0 -difficulty 16
```

### Configure Batch Size

Batch size is specified as a power of 10:
- `4` = 10,000 nonces per batch
- `5` = 100,000 nonces per batch
- `6` = 1,000,000 nonces per batch
- `7` = 10,000,000 nonces per batch
- Up to `10` = 10,000,000,000 nonces per batch

```bash
./gpu-nostr-pow -batch-size 5 -difficulty 16
```

Use `-1` (default) for auto-detection based on difficulty.

**Note**: For CPU devices, batch sizes larger than 10^4 (10,000) may cause segmentation faults. The benchmark automatically limits CPU batch sizes to prevent this.

### Select Kernel

Choose a specific kernel implementation:

```bash
./gpu-nostr-pow -kernel ckolivas -difficulty 16
```

Available kernels: `default`, `ckolivas`, or `auto` (default, selects based on device).

### Verbose Logging

```bash
./gpu-nostr-pow -verbose -difficulty 16
```

In verbose mode, the selected kernel is printed to stderr.

### Benchmark All Kernels

Test all kernels and batch sizes to find the optimal configuration:

```bash
./gpu-nostr-pow -benchmark
```

This will:
- Test both kernel implementations (default and ckolivas)
- For each kernel, test batch sizes from 1,000 (10^3) to 10,000,000,000 (10^10)
- For CPU devices, batch size is limited to 10,000 (10^4) to avoid segfaults
- Run each combination 3 times (5 seconds each) with different events
- Display a summary table with the best batch size for each kernel
- Provide a final recommendation with the best kernel and batch size

Example output:
```
=== Benchmark Summary ===
Kernel       Best Batch Size      Performance
------       -------------      ------------
default      10^5                1.25M nonces/s
ckolivas     10^6                2.80M nonces/s
phatk        10^5                1.15M nonces/s
...

=== Recommendation ===
Best kernel: ckolivas
Best batch size: 10^6 (1000000)
Performance: 2.80M nonces/s

Use: -kernel ckolivas -batch-size 6
```

### Test Kernel Correctness

Verify that all kernels produce correct results:

```bash
./gpu-nostr-pow -test-kernels -difficulty 20
```

This will:
- Test each kernel 10 times with random events
- Report correct/wrong/error counts for each kernel
- Display a summary table at the end
- Useful for validating kernel implementations after modifications

## Command-Line Options

- `-difficulty <n>`: Number of leading zero bits required (default: 16)
- `-batch-size <n>`: Batch size as power of 10 (4=10000, 5=100000, etc.). Use -1 for auto-detect (default: -1). Maximum: 10 (10^10)
- `-kernel <name>`: Kernel implementation to use: `auto` (default, selects based on device), `default`, or `ckolivas`
- `-list-devices`, `-l`: List available OpenCL devices and exit
- `-device <n>`, `-d <n>`: Select device by index from list
- `-benchmark`: Test all kernels and batch sizes to find optimal configuration
- `-test-kernels`: Test all kernels with random events to verify correctness
- `-verbose`: Enable verbose logging (shows selected kernel)

## How It Works

1. Reads a Nostr event JSON from stdin
2. Calculates the required number of leading zero bits based on difficulty
3. Selects an appropriate OpenCL kernel (automatically or manually)
4. Uses OpenCL to test nonces in parallel batches on the GPU/CPU
5. Validates candidate nonces on the CPU to ensure correctness
6. Finds a nonce that produces the required number of leading zero bits
7. Outputs the event JSON with the `nonce` tag and updated `id` field

The miner dynamically adjusts the number of digits in the nonce based on the difficulty level, ensuring sufficient range to find valid nonces. The OpenCL kernel returns only the index of a found nonce, reducing memory bandwidth by ~90% compared to returning full hash results.

## Kernel Organization

All OpenCL kernel files are organized in the `kernel/` directory:

- **Original kernels**: Reference files from upstream projects
  - `ckolivas.cl` - Original from sgminer
  
- **Adapted kernels**: Modified versions for NIP-13 mining (used in compilation)
  - `mine.cl` - Our original implementation
  - `ckolivas-adapted.cl` - Adapted from sgminer's ckolivas

Each adapted kernel includes comments indicating:
- That it was modified from the original
- Link to the original source repository
- Description of changes made

## License

[Add your license here]

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

