package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"unsafe"

	cl "github.com/jgillich/go-opencl/cl"
)

func main() {
	// Get platforms
	platforms, err := cl.GetPlatforms()
	if err != nil {
		log.Fatalf("Failed to get platforms: %v", err)
	}

	if len(platforms) == 0 {
		log.Fatal("No OpenCL platforms found")
	}

	// Get devices
	devices, err := platforms[0].GetDevices(cl.DeviceTypeAll)
	if err != nil {
		log.Fatalf("Failed to get devices: %v", err)
	}

	if len(devices) == 0 {
		log.Fatal("No OpenCL devices found")
	}

	// Create context
	context, err := cl.CreateContext([]*cl.Device{devices[0]})
	if err != nil {
		log.Fatalf("Failed to create context: %v", err)
	}
	defer context.Release()

	// Create command queue
	queue, err := context.CreateCommandQueue(devices[0], 0)
	if err != nil {
		log.Fatalf("Failed to create command queue: %v", err)
	}
	defer queue.Release()

	// Read kernel source from file
	kernelSource, err := os.ReadFile("sha256.cl")
	if err != nil {
		log.Fatalf("Failed to read kernel file: %v", err)
	}

	// Create program
	program, err := context.CreateProgramWithSource([]string{string(kernelSource)})
	if err != nil {
		log.Fatalf("Failed to create program: %v", err)
	}
	defer program.Release()

	// Build program
	err = program.BuildProgram(nil, "")
	if err != nil {
		log.Fatalf("Failed to build program: %v", err)
	}

	// Create kernel
	kernel, err := program.CreateKernel("sha256")
	if err != nil {
		log.Fatalf("Failed to create kernel: %v", err)
	}
	defer kernel.Release()

	// Read input string from stdin
	inputBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read from stdin: %v", err)
	}

	if len(inputBytes) == 0 {
		log.Fatal("No input provided")
	}

	// Check if input is too long (current kernel handles up to 55 bytes)
	if len(inputBytes) > 55 {
		log.Fatalf("Input too long: %d bytes (maximum 55 bytes supported)", len(inputBytes))
	}

	inputLength := len(inputBytes)
	inputStr := string(inputBytes)

	// SHA256 produces 32 bytes (256 bits) output
	const sha256OutputSize = 32

	// Create input buffer (empty, we'll write to it)
	inputBuffer, err := context.CreateEmptyBuffer(cl.MemReadOnly, len(inputBytes))
	if err != nil {
		log.Fatalf("Failed to create input buffer: %v", err)
	}
	defer inputBuffer.Release()

	// Create output buffer for SHA256 hash (32 bytes)
	outputBuffer, err := context.CreateEmptyBuffer(cl.MemWriteOnly, sha256OutputSize)
	if err != nil {
		log.Fatalf("Failed to create output buffer: %v", err)
	}
	defer outputBuffer.Release()

	// Write input data to buffer
	_, err = queue.EnqueueWriteBuffer(inputBuffer, true, 0, len(inputBytes), unsafe.Pointer(&inputBytes[0]), nil)
	if err != nil {
		log.Fatalf("Failed to write input buffer: %v", err)
	}

	// Set kernel arguments
	err = kernel.SetArgBuffer(0, inputBuffer)
	if err != nil {
		log.Fatalf("Failed to set kernel arg 0: %v", err)
	}

	err = kernel.SetArgBuffer(1, outputBuffer)
	if err != nil {
		log.Fatalf("Failed to set kernel arg 1: %v", err)
	}

	err = kernel.SetArgInt32(2, int32(inputLength))
	if err != nil {
		log.Fatalf("Failed to set kernel arg 2: %v", err)
	}

	// Execute kernel
	globalSize := []int{1}
	_, err = queue.EnqueueNDRangeKernel(kernel, nil, globalSize, nil, nil)
	if err != nil {
		log.Fatalf("Failed to enqueue kernel: %v", err)
	}

	// Read result (32 bytes for SHA256)
	result := make([]byte, sha256OutputSize)
	_, err = queue.EnqueueReadBuffer(outputBuffer, true, 0, sha256OutputSize, unsafe.Pointer(&result[0]), nil)
	if err != nil {
		log.Fatalf("Failed to read buffer: %v", err)
	}

	// Print result
	fmt.Printf("Input string: %s\n", inputStr)
	fmt.Printf("SHA256 hash: ")
	for _, b := range result {
		fmt.Printf("%02x", b)
	}
	fmt.Println()
}
