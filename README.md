# GPU NIP-13 Miner

A high-performance GPU-accelerated miner for NIP-13 proof-of-work (PoW) on Nostr. This tool uses OpenCL to leverage GPU compute power for mining Nostr events with configurable difficulty levels.

## Features

- **GPU Acceleration**: Uses OpenCL to mine on GPUs (NVIDIA, Intel, AMD) or CPUs
- **Dynamic Batch Sizing**: Automatically optimizes batch size based on difficulty, or configure manually
- **Device Selection**: List and select specific OpenCL devices
- **Progress Tracking**: Real-time progress bar showing nonce rate and percentage relative to expected iterations
- **Cross-Platform**: Works on Linux, Windows, and macOS

## Performance Benchmarks

Performance varies significantly based on the OpenCL device used:

### CPU Performance
**~1M nonces/s** with Intel Core i5-9500T CPU
```
Platform 0: Portable Computing Language (The pocl project)
  [0] cpu-haswell-Intel(R) Core(TM) i5-9500T CPU @ 2.20GHz (GenuineIntel) - CPU
       Version: OpenCL 3.0 PoCL HSTR: cpu-x86_64-pc-linux-gnu-haswell
       Compute Units: 6, Work Group Size: 4096, Memory: 13850 MB
```

### Intel GPU Performance
**~3M nonces/s** with Intel UHD Graphics 630
```
Platform 0: Intel(R) OpenCL HD Graphics (Intel(R) Corporation)
  [0] Intel(R) UHD Graphics 630 (Intel(R) Corporation) - GPU
       Version: OpenCL 3.0 NEO
       Compute Units: 24, Work Group Size: 256, Memory: 13030 MB
```

### NVIDIA GPU Performance
**~28M nonces/s** with NVIDIA GeForce GTX 1660 Ti
```
Platform 0: NVIDIA CUDA (NVIDIA Corporation)
  [0] NVIDIA GeForce GTX 1660 Ti (NVIDIA Corporation) - GPU
       Version: OpenCL 3.0 CUDA
       Compute Units: 24, Work Group Size: 1024, Memory: 6143 MB
```

## Requirements

- **Go 1.21+** (for building from source)
- **OpenCL** runtime and development headers
  - **Linux**: Install `ocl-icd-opencl-dev` or vendor-specific OpenCL packages
  - **Windows**: OpenCL.dll (usually included with GPU drivers)
  - **macOS**: OpenCL framework (included by default)

## Building

### Linux

```bash
go build -o gpu-nostr-pow
```

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

### Cross-compilation from Linux

```bash
./build-windows.sh
```

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

```bash
./gpu-nostr-pow -batch-size 5 -difficulty 16
```

Use `-1` (default) for auto-detection based on difficulty.

### Verbose Logging

```bash
./gpu-nostr-pow -verbose -difficulty 16
```

## Command-Line Options

- `-difficulty <n>`: Number of leading zero bits required (default: 16)
- `-batch-size <n>`: Batch size as power of 10 (4=10000, 5=100000, etc.). Use -1 for auto-detect (default: -1)
- `-list-devices`, `-l`: List available OpenCL devices and exit
- `-device <n>`, `-d <n>`: Select device by index from list
- `-verbose`: Enable verbose logging

## How It Works

1. Reads a Nostr event JSON from stdin
2. Calculates the required number of leading zero bits based on difficulty
3. Uses OpenCL to test nonces in parallel batches
4. Finds a nonce that produces the required number of leading zero bits
5. Outputs the event JSON with the `nonce` and `pow` fields populated

The miner dynamically adjusts the number of digits in the nonce based on the difficulty level, ensuring sufficient range to find valid nonces.

## License

[Add your license here]

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

