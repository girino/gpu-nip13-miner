package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strconv"
	"time"
	"unsafe"

	cl "github.com/jgillich/go-opencl/cl"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip13"
)

var verbose bool

func vlog(format string, args ...interface{}) {
	if verbose {
		log.Printf(format, args...)
	}
}

// selectKernelForDevice automatically selects the best kernel for a given device
// Returns "default" for CPUs and Intel GPUs, "ckolivas" for other GPUs
func selectKernelForDevice(device *cl.Device) string {
	deviceType := device.Type()
	deviceVendor := device.Vendor()

	// CPUs always use default kernel
	if (deviceType & cl.DeviceTypeCPU) != 0 {
		return "default"
	}

	// Intel GPUs use default kernel
	if (deviceType & cl.DeviceTypeGPU) != 0 {
		// Check if vendor contains "Intel" (case-insensitive check)
		vendorLower := ""
		for _, r := range deviceVendor {
			vendorLower += string(r | 0x20) // Convert to lowercase
		}
		// Check if "intel" is contained in the vendor string
		if len(vendorLower) >= 5 {
			for i := 0; i <= len(vendorLower)-5; i++ {
				if vendorLower[i:i+5] == "intel" {
					return "default"
				}
			}
		}
		// Other GPUs (NVIDIA, AMD, etc.) use ckolivas
		return "ckolivas"
	}

	// Default fallback
	return "default"
}

// getKernelSource returns the kernel source code based on the kernel type
// If kernelType is "auto", it will be determined based on the device
func getKernelSource(kernelType string, device *cl.Device) (string, string, error) {
	// Auto-select kernel based on device if "auto" is specified
	if kernelType == "auto" {
		if device == nil {
			return "", "", fmt.Errorf("device is required for auto kernel selection")
		}
		kernelType = selectKernelForDevice(device)
	}

	switch kernelType {
	case "default":
		return mineKernelSource, "mine_nonce", nil
	case "ckolivas":
		// Note: ckolivas kernel is experimental and may not work correctly
		// It's based on sgminer's Scrypt implementation, adapted for NIP-13
		return ckolivasKernelSource, "mine_nonce", nil
	case "phatk":
		// bfgminer phatk kernel adapted for NIP-13 mining
		return bfgminerPhatkKernelSource, "mine_nonce", nil
	case "diakgcn":
		// bfgminer diakgcn kernel adapted for NIP-13 mining
		return bfgminerDiakgcnKernelSource, "mine_nonce", nil
	case "diablo":
		// bfgminer diablo kernel adapted for NIP-13 mining
		return bfgminerDiabloKernelSource, "mine_nonce", nil
	case "poclbm":
		// bfgminer poclbm kernel adapted for NIP-13 mining
		return bfgminerPoclbmKernelSource, "mine_nonce", nil
	default:
		return "", "", fmt.Errorf("unknown kernel type: %s (use 'default', 'ckolivas', 'phatk', 'diakgcn', 'diablo', 'poclbm', or 'auto')", kernelType)
	}
}

// validateNonce validates a candidate nonce by recalculating the hash on CPU.
// Returns true if valid, false otherwise.
// Logs errors to stderr.
func validateNonce(candidateNonce uint64, event *nostr.Event, difficulty int, numDigits int) bool {
	// Create a deep copy of the event for validation
	testEvent := *event
	// Clear the ID so it gets recalculated
	testEvent.ID = ""
	// Deep copy tags to avoid modifying the original
	testEvent.Tags = make(nostr.Tags, len(event.Tags))
	for i, tag := range event.Tags {
		testEvent.Tags[i] = make(nostr.Tag, len(tag))
		copy(testEvent.Tags[i], tag)
	}

	// Format nonce with correct number of digits
	nonceStr := fmt.Sprintf("%0*d", numDigits, candidateNonce)

	// Find and update nonce tag (remove old one first, then add new)
	filteredTags := make(nostr.Tags, 0, len(testEvent.Tags))
	for _, tag := range testEvent.Tags {
		if len(tag) == 0 || tag[0] != "nonce" {
			filteredTags = append(filteredTags, tag)
		}
	}
	// Add new nonce tag
	testEvent.Tags = append(filteredTags, nostr.Tag{"nonce", nonceStr, strconv.Itoa(difficulty)})

	// Recalculate event ID by serializing and hashing (CPU-side validation)
	eventIDHex := testEvent.GetID()

	// Set the event ID (required for CommittedDifficulty to work correctly)
	testEvent.ID = eventIDHex

	// Validate difficulty using NIP-13 Check function
	if err := nip13.Check(eventIDHex, difficulty); err != nil {
		fmt.Fprintf(os.Stderr, "Validation error: NIP-13 validation failed: %v (nonce: %d). Continuing...\n",
			err, candidateNonce)
		return false
	}

	// Additional validation: check committed difficulty matches
	// Note: CommittedDifficulty reads from the nonce tag's third element
	// It compares the tag difficulty with the actual hash difficulty
	// If tag difficulty > actual difficulty, it returns 0
	actualHashDifficulty := nip13.Difficulty(eventIDHex)
	committedDiff := nip13.CommittedDifficulty(&testEvent)

	// CommittedDifficulty should return the minimum of tag difficulty and actual difficulty
	// But if tag difficulty > actual difficulty, it returns 0
	// So we need to check if the actual difficulty meets our requirement
	if actualHashDifficulty < difficulty {
		fmt.Fprintf(os.Stderr, "Validation error: Hash difficulty %d is less than required %d (nonce: %d). Continuing...\n",
			actualHashDifficulty, difficulty, candidateNonce)
		return false
	}

	// If committedDiff is 0, it means tag difficulty > actual difficulty
	// In that case, we should still accept if actual difficulty >= required difficulty
	if committedDiff == 0 && actualHashDifficulty >= difficulty {
		// This is OK - the hash meets the requirement even if tag says higher
		// But we should update the tag to match the actual difficulty
		// For now, just accept it
		return true
	}

	if committedDiff != difficulty && committedDiff != 0 {
		// Debug: check what tags we have
		var nonceTagFound bool
		for _, tag := range testEvent.Tags {
			if len(tag) > 0 && tag[0] == "nonce" {
				nonceTagFound = true
				if len(tag) < 3 {
					fmt.Fprintf(os.Stderr, "Validation error: Nonce tag has wrong format (len=%d, expected 3): %v (nonce: %d). Continuing...\n",
						len(tag), tag, candidateNonce)
				} else {
					fmt.Fprintf(os.Stderr, "Validation error: Committed difficulty mismatch! Expected: %d, Got: %d, Actual hash difficulty: %d, Tag: %v (nonce: %d). Continuing...\n",
						difficulty, committedDiff, actualHashDifficulty, tag, candidateNonce)
				}
				break
			}
		}
		if !nonceTagFound {
			fmt.Fprintf(os.Stderr, "Validation error: Nonce tag not found in event! (nonce: %d). Continuing...\n", candidateNonce)
		}
		return false
	}

	// All validations passed
	return true
}

func updateProgressBar(nonce int64, digits int, totalTested int64, startTime time.Time, difficulty int) {
	elapsed := time.Since(startTime)
	var rate float64
	if elapsed.Seconds() > 0 {
		rate = float64(totalTested) / elapsed.Seconds()
	}

	// Calculate expected iterations: 2^difficulty
	expectedIterations := math.Pow(2, float64(difficulty))

	// Calculate percentage relative to expected iterations
	// Allow percentage to exceed 100% to show how much beyond expected we are
	var percent float64
	if expectedIterations > 0 {
		percent = float64(totalTested) / expectedIterations * 100
	}

	// Format rate
	var rateStr string
	if rate >= 1000000 {
		rateStr = fmt.Sprintf("%.2fM", rate/1000000)
	} else if rate >= 1000 {
		rateStr = fmt.Sprintf("%.2fK", rate/1000)
	} else {
		rateStr = fmt.Sprintf("%.0f", rate)
	}

	// Format elapsed time
	elapsedSec := int(elapsed.Seconds())
	hours := elapsedSec / 3600
	minutes := (elapsedSec % 3600) / 60
	seconds := elapsedSec % 60
	var elapsedStr string
	if hours > 0 {
		elapsedStr = fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
	} else if minutes > 0 {
		elapsedStr = fmt.Sprintf("%dm%ds", minutes, seconds)
	} else {
		elapsedStr = fmt.Sprintf("%ds", seconds)
	}

	// Print progress bar to stderr
	fmt.Fprintf(os.Stderr, "\r[%d digits] Nonce: %d (%.1f%% of expected) | Rate: %s nonces/s | Elapsed: %s",
		digits, nonce, percent, rateStr, elapsedStr)
	os.Stderr.Sync() // Flush stderr to ensure it's visible
}

func listAllDevices() {
	platforms, err := cl.GetPlatforms()
	if err != nil {
		log.Fatalf("Failed to get platforms: %v", err)
	}

	if len(platforms) == 0 {
		log.Fatal("No OpenCL platforms found")
	}

	var allDevices []*cl.Device
	var devicePlatforms []int // Track which platform each device belongs to

	fmt.Println("Available OpenCL devices:")
	fmt.Println()

	deviceNum := 0
	for platformIdx, platform := range platforms {
		platformName := platform.Name()
		platformVendor := platform.Vendor()
		fmt.Printf("Platform %d: %s (%s)\n", platformIdx, platformName, platformVendor)

		devices, err := platform.GetDevices(cl.DeviceTypeAll)
		if err != nil {
			fmt.Printf("  Error getting devices: %v\n", err)
			continue
		}

		for _, device := range devices {
			deviceName := device.Name()
			deviceVendor := device.Vendor()
			deviceType := device.Type()
			deviceVersion := device.Version()
			maxComputeUnits := device.MaxComputeUnits()
			maxWorkGroupSize := device.MaxWorkGroupSize()
			globalMemSize := device.GlobalMemSize()

			var typeStr string
			if (deviceType & cl.DeviceTypeGPU) != 0 {
				typeStr = "GPU"
			} else if (deviceType & cl.DeviceTypeCPU) != 0 {
				typeStr = "CPU"
			} else {
				typeStr = "Other"
			}

			fmt.Printf("  [%d] %s (%s) - %s\n", deviceNum, deviceName, deviceVendor, typeStr)
			fmt.Printf("       Version: %s\n", deviceVersion)
			fmt.Printf("       Compute Units: %d, Work Group Size: %d, Memory: %d MB\n",
				maxComputeUnits, maxWorkGroupSize, globalMemSize/(1024*1024))
			fmt.Println()

			allDevices = append(allDevices, device)
			devicePlatforms = append(devicePlatforms, platformIdx)
			deviceNum++
		}
	}

	if len(allDevices) == 0 {
		log.Fatal("No OpenCL devices found")
	}

	os.Exit(0)
}

// createRealisticBenchmarkEvent creates a realistic Nostr event with random values
// This makes the benchmark more representative of real mining scenarios
func createRealisticBenchmarkEvent() nostr.Event {
	// Generate random pubkey (32 bytes)
	pubkeyBytes := make([]byte, 32)
	rand.Read(pubkeyBytes)
	pubkey := hex.EncodeToString(pubkeyBytes)

	// Generate random content (varying length like real events)
	contentLengths := []int{50, 100, 200, 500, 1000}
	contentBytes := make([]byte, contentLengths[len(pubkeyBytes)%len(contentLengths)])
	rand.Read(contentBytes)
	content := hex.EncodeToString(contentBytes)

	// Create realistic tags (like #p, #e, #t tags that are common in Nostr)
	tags := nostr.Tags{
		nostr.Tag{"p", pubkey, "wss://relay.example.com"},
	}

	// Randomly add more tags (30% chance)
	if len(pubkeyBytes)%10 < 3 {
		// Add another pubkey tag
		anotherPubkey := make([]byte, 32)
		rand.Read(anotherPubkey)
		tags = append(tags, nostr.Tag{"p", hex.EncodeToString(anotherPubkey), ""})
	}

	// Randomly add event reference tag (20% chance)
	if len(pubkeyBytes)%10 < 2 {
		eventRef := make([]byte, 32)
		rand.Read(eventRef)
		tags = append(tags, nostr.Tag{"e", hex.EncodeToString(eventRef), "wss://relay.example.com"})
	}

	// Randomly add topic tags (40% chance)
	if len(pubkeyBytes)%10 < 4 {
		topics := []string{"bitcoin", "nostr", "opencl", "gpu", "mining", "crypto", "tech"}
		topic := topics[len(pubkeyBytes)%len(topics)]
		tags = append(tags, nostr.Tag{"t", topic})
	}

	// Create event with random values
	event := nostr.Event{
		Kind:      1, // Text note
		Content:   content,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
	}

	// Set a random pubkey (we'll use a deterministic one for consistency in benchmark)
	// But make it look realistic
	event.PubKey = pubkey

	return event
}

// runBenchmark tests all kernels and different batch sizes to find the optimal combination
func runBenchmark(difficulty int, deviceIndex int, kernelType string) {
	fmt.Fprintf(os.Stderr, "Running benchmark to find optimal kernel and batch size...\n")
	fmt.Fprintf(os.Stderr, "Each kernel and batch size will be tested 3 times (5 seconds each) with different events.\n\n")

	// Get platforms
	platforms, err := cl.GetPlatforms()
	if err != nil {
		log.Fatalf("Failed to get platforms: %v", err)
	}

	if len(platforms) == 0 {
		log.Fatal("No OpenCL platforms found")
	}

	// Collect all devices
	var allDevices []*cl.Device
	for _, platform := range platforms {
		devices, err := platform.GetDevices(cl.DeviceTypeAll)
		if err != nil {
			continue
		}
		for _, device := range devices {
			allDevices = append(allDevices, device)
		}
	}

	if len(allDevices) == 0 {
		log.Fatal("No OpenCL devices found")
	}

	// Select device
	var selectedDevice *cl.Device
	if deviceIndex >= 0 {
		if deviceIndex >= len(allDevices) {
			log.Fatalf("Device index %d is out of range (0-%d)", deviceIndex, len(allDevices)-1)
		}
		selectedDevice = allDevices[deviceIndex]
	} else {
		// Auto-select GPU or first device
		selectedDevice = nil
		for _, device := range allDevices {
			deviceType := device.Type()
			if (deviceType & cl.DeviceTypeGPU) != 0 {
				selectedDevice = device
				break
			}
		}
		if selectedDevice == nil {
			selectedDevice = allDevices[0]
		}
	}

	deviceName := selectedDevice.Name()
	deviceType := selectedDevice.Type()
	isCPU := (deviceType & cl.DeviceTypeCPU) != 0

	fmt.Fprintf(os.Stderr, "Testing on device: %s\n", deviceName)
	if isCPU {
		fmt.Fprintf(os.Stderr, "Note: Batch size limited to 10^4 for CPU to avoid segfaults.\n")
	}
	fmt.Fprintf(os.Stderr, "\n")

	// Test all kernels
	kernels := []string{"default", "ckolivas", "phatk", "diakgcn", "diablo", "poclbm"}

	type kernelBenchmarkResult struct {
		kernelName     string
		bestBatchPower int
		bestBatchSize  int
		bestRate       float64
	}

	var kernelResults []kernelBenchmarkResult

	// Determine max batch size power based on device type
	maxPower := 10
	if isCPU {
		maxPower = 4 // Limit to 10^4 for CPU
	}

	for _, kernel := range kernels {
		fmt.Fprintf(os.Stderr, "=== Testing kernel: %s ===\n", kernel)

		type benchmarkResult struct {
			batchSizePower int
			batchSize      int
			rate           float64
		}

		var results []benchmarkResult

		// Test batch sizes from 3 to maxPower
		for power := 3; power <= maxPower; power++ {
			batchSize := int(math.Pow(10, float64(power)))

			fmt.Fprintf(os.Stderr, "  Testing batch size 10^%d (%d)... ", power, batchSize)

			// Run benchmark 3 times with different events for more reliable results
			var rates []float64
			failed := false
			for run := 0; run < 3; run++ {
				// Create a new realistic event for each run
				testEvent := createRealisticBenchmarkEvent()

				// Run benchmark for this batch size (5 seconds per run)
				rate, err := benchmarkBatchSizeSafe(selectedDevice, &testEvent, difficulty, batchSize, kernel)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\n  Error testing batch size 10^%d (%d): %v\n", power, batchSize, err)
					fmt.Fprintf(os.Stderr, "  Batch size too large for this device/kernel. Stopping.\n")
					failed = true
					break
				}
				rates = append(rates, rate)

				if run < 2 {
					fmt.Fprintf(os.Stderr, "%.2fM ", rate/1000000)
				}
			}

			if failed {
				// Stop testing larger batch sizes if we hit an error
				break
			}

			// Calculate average rate
			avgRate := 0.0
			for _, r := range rates {
				avgRate += r
			}
			avgRate /= float64(len(rates))

			results = append(results, benchmarkResult{
				batchSizePower: power,
				batchSize:      batchSize,
				rate:           avgRate,
			})

			fmt.Fprintf(os.Stderr, "%.2fM nonces/s (avg of 3 runs)\n", avgRate/1000000)
		}

		// Find best batch size for this kernel
		if len(results) == 0 {
			fmt.Fprintf(os.Stderr, "  No valid batch sizes for kernel %s\n\n", kernel)
			continue
		}

		best := results[0]
		for _, r := range results {
			if r.rate > best.rate {
				best = r
			}
		}

		kernelResults = append(kernelResults, kernelBenchmarkResult{
			kernelName:     kernel,
			bestBatchPower: best.batchSizePower,
			bestBatchSize:  best.batchSize,
			bestRate:       best.rate,
		})

		fmt.Fprintf(os.Stderr, "  Best for %s: batch size 10^%d (%d) = %.2fM nonces/s\n\n", kernel, best.batchSizePower, best.batchSize, best.rate/1000000)
	}

	// Print summary table
	if len(kernelResults) == 0 {
		log.Fatal("No valid kernel results found")
	}

	fmt.Fprintf(os.Stderr, "=== Benchmark Summary ===\n")
	fmt.Fprintf(os.Stderr, "%-12s %12s %20s\n", "Kernel", "Best Batch Size", "Performance")
	fmt.Fprintf(os.Stderr, "%-12s %12s %20s\n", "------", "-------------", "-----------")
	for _, kr := range kernelResults {
		fmt.Fprintf(os.Stderr, "%-12s 10^%-8d %-8.2fM nonces/s\n",
			kr.kernelName, kr.bestBatchPower, kr.bestRate/1000000)
	}
	fmt.Fprintf(os.Stderr, "\n")

	// Find overall best kernel
	bestKernel := kernelResults[0]
	for _, kr := range kernelResults {
		if kr.bestRate > bestKernel.bestRate {
			bestKernel = kr
		}
	}

	fmt.Fprintf(os.Stderr, "=== Recommendation ===\n")
	fmt.Fprintf(os.Stderr, "Best kernel: %s\n", bestKernel.kernelName)
	fmt.Fprintf(os.Stderr, "Best batch size: 10^%d (%d)\n", bestKernel.bestBatchPower, bestKernel.bestBatchSize)
	fmt.Fprintf(os.Stderr, "Performance: %.2fM nonces/s\n", bestKernel.bestRate/1000000)
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Use: -kernel %s -batch-size %d\n", bestKernel.kernelName, bestKernel.bestBatchPower)
}

// testSingleKernel tests a single kernel by mining a random event and validating the result
// Returns true if the mined nonce is valid, false otherwise
func testSingleKernel(device *cl.Device, event *nostr.Event, difficulty int, kernelType string) (bool, uint64, error) {
	// Use a reasonable batch size for testing (10^4 = 10000)
	batchSize := 10000

	// Create context
	context, err := cl.CreateContext([]*cl.Device{device})
	if err != nil {
		return false, 0, fmt.Errorf("failed to create context: %v", err)
	}
	defer context.Release()

	// Create command queue
	queue, err := context.CreateCommandQueue(device, 0)
	if err != nil {
		return false, 0, fmt.Errorf("failed to create command queue: %v", err)
	}
	defer queue.Release()

	// Get kernel source
	kernelSource, kernelName, err := getKernelSource(kernelType, device)
	if err != nil {
		return false, 0, err
	}

	// Create program
	program, err := context.CreateProgramWithSource([]string{kernelSource})
	if err != nil {
		return false, 0, fmt.Errorf("failed to create program: %v", err)
	}
	defer program.Release()

	// Build program
	err = program.BuildProgram(nil, "")
	if err != nil {
		return false, 0, fmt.Errorf("failed to build program: %v", err)
	}

	// Create kernel
	kernel, err := program.CreateKernel(kernelName)
	if err != nil {
		return false, 0, fmt.Errorf("failed to create kernel: %v", err)
	}
	defer kernel.Release()

	// Calculate number of digits needed
	expectedAttempts := math.Pow(2, float64(difficulty))
	numDigits := int(math.Ceil(math.Log10(expectedAttempts))) + 2
	if numDigits < 10 {
		numDigits = 10
	}

	// Calculate max batches based on difficulty: expected attempts = 2^difficulty
	// Use 3x expected value to account for variance and ensure high success rate
	maxBatches := int(math.Ceil((expectedAttempts * 3) / float64(batchSize)))
	if maxBatches < 10 {
		maxBatches = 10 // Minimum 10 batches
	}

	// Prepare event with placeholder nonce
	testEvent := *event
	noncePlaceholder := fmt.Sprintf("%0*d", numDigits, 0)
	testEvent.Tags = append(testEvent.Tags, nostr.Tag{"nonce", noncePlaceholder, strconv.Itoa(difficulty)})

	// Serialize event
	serialized := testEvent.Serialize()
	serializedLength := len(serialized)

	// Find nonce position
	noncePlaceholderBytes := []byte(noncePlaceholder)
	nonceOffset := bytes.Index(serialized, noncePlaceholderBytes)
	if nonceOffset == -1 {
		return false, 0, fmt.Errorf("could not find nonce placeholder in serialized event")
	}

	// Create buffers
	inputBuffer, err := context.CreateEmptyBuffer(cl.MemReadOnly, serializedLength)
	if err != nil {
		return false, 0, fmt.Errorf("failed to create input buffer: %v", err)
	}
	defer inputBuffer.Release()

	resultSize := 4 // int32
	resultsBufferSize := batchSize * resultSize
	resultsBuffer, err := context.CreateEmptyBuffer(cl.MemWriteOnly, resultsBufferSize)
	if err != nil {
		return false, 0, fmt.Errorf("failed to create results buffer: %v", err)
	}
	defer resultsBuffer.Release()

	// Write input buffer
	_, err = queue.EnqueueWriteBuffer(inputBuffer, true, 0, serializedLength, unsafe.Pointer(&serialized[0]), nil)
	if err != nil {
		return false, 0, fmt.Errorf("failed to write input buffer: %v", err)
	}

	// Set kernel arguments
	err = kernel.SetArgBuffer(0, inputBuffer)
	if err != nil {
		return false, 0, fmt.Errorf("failed to set kernel arg 0: %v", err)
	}

	err = kernel.SetArgInt32(1, int32(serializedLength))
	if err != nil {
		return false, 0, fmt.Errorf("failed to set kernel arg 1: %v", err)
	}

	err = kernel.SetArgInt32(2, int32(nonceOffset))
	if err != nil {
		return false, 0, fmt.Errorf("failed to set kernel arg 2: %v", err)
	}

	err = kernel.SetArgInt32(3, int32(difficulty))
	if err != nil {
		return false, 0, fmt.Errorf("failed to set kernel arg 3: %v", err)
	}

	baseNonceLow := uint32(0)
	baseNonceHigh := uint32(0)
	err = kernel.SetArgInt32(4, int32(baseNonceLow))
	if err != nil {
		return false, 0, fmt.Errorf("failed to set kernel arg 4: %v", err)
	}

	err = kernel.SetArgInt32(5, int32(baseNonceHigh))
	if err != nil {
		return false, 0, fmt.Errorf("failed to set kernel arg 5: %v", err)
	}

	err = kernel.SetArgBuffer(6, resultsBuffer)
	if err != nil {
		return false, 0, fmt.Errorf("failed to set kernel arg 6: %v", err)
	}

	err = kernel.SetArgInt32(7, int32(numDigits))
	if err != nil {
		return false, 0, fmt.Errorf("failed to set kernel arg 7: %v", err)
	}

	// Execute kernel multiple times until we find a valid nonce or exhaust attempts
	for batch := 0; batch < maxBatches; batch++ {
		baseNonce := int64(batch) * int64(batchSize)
		baseNonceLow = uint32(baseNonce & 0xFFFFFFFF)
		baseNonceHigh = uint32((baseNonce >> 32) & 0xFFFFFFFF)

		err = kernel.SetArgInt32(4, int32(baseNonceLow))
		if err != nil {
			return false, 0, fmt.Errorf("failed to set kernel arg 4: %v", err)
		}

		err = kernel.SetArgInt32(5, int32(baseNonceHigh))
		if err != nil {
			return false, 0, fmt.Errorf("failed to set kernel arg 5: %v", err)
		}

		// Execute kernel
		globalSize := []int{batchSize}
		_, err = queue.EnqueueNDRangeKernel(kernel, nil, globalSize, nil, nil)
		if err != nil {
			return false, 0, fmt.Errorf("failed to enqueue kernel: %v", err)
		}

		// Read results
		results := make([]byte, resultsBufferSize)
		_, err = queue.EnqueueReadBuffer(resultsBuffer, true, 0, resultsBufferSize, unsafe.Pointer(&results[0]), nil)
		if err != nil {
			return false, 0, fmt.Errorf("failed to read results buffer: %v", err)
		}

		// Check results
		resultIndices := (*[1 << 28]int32)(unsafe.Pointer(&results[0]))[:batchSize:batchSize]
		for i := 0; i < batchSize; i++ {
			index := resultIndices[i]
			if index >= 0 {
				candidateNonce := uint64(baseNonce) + uint64(index)
				// Validate the nonce
				if validateNonce(candidateNonce, event, difficulty, numDigits) {
					return true, candidateNonce, nil
				}
			}
		}
	}

	return false, 0, fmt.Errorf("no valid nonce found after %d batches", maxBatches)
}

// testAllKernels tests all available kernels with random events
func testAllKernels(difficulty int, deviceIndex int) {
	fmt.Fprintf(os.Stderr, "Testing all kernels with difficulty %d...\n", difficulty)
	fmt.Fprintf(os.Stderr, "Each kernel will be tested 10 times with random events.\n\n")

	// Get platforms
	platforms, err := cl.GetPlatforms()
	if err != nil {
		log.Fatalf("Failed to get platforms: %v", err)
	}

	if len(platforms) == 0 {
		log.Fatal("No OpenCL platforms found")
	}

	// Collect all devices
	var allDevices []*cl.Device
	for _, platform := range platforms {
		devices, err := platform.GetDevices(cl.DeviceTypeAll)
		if err != nil {
			continue
		}
		for _, device := range devices {
			allDevices = append(allDevices, device)
		}
	}

	if len(allDevices) == 0 {
		log.Fatal("No OpenCL devices found")
	}

	// Select device
	var selectedDevice *cl.Device
	if deviceIndex >= 0 {
		if deviceIndex >= len(allDevices) {
			log.Fatalf("Device index %d is out of range (0-%d)", deviceIndex, len(allDevices)-1)
		}
		selectedDevice = allDevices[deviceIndex]
	} else {
		// Auto-select GPU or first device
		selectedDevice = nil
		for _, device := range allDevices {
			deviceType := device.Type()
			if (deviceType & cl.DeviceTypeGPU) != 0 {
				selectedDevice = device
				break
			}
		}
		if selectedDevice == nil {
			selectedDevice = allDevices[0]
		}
	}

	deviceName := selectedDevice.Name()
	fmt.Fprintf(os.Stderr, "Testing on device: %s\n\n", deviceName)

	// List of all kernels to test
	kernels := []string{"default", "ckolivas", "phatk", "diakgcn", "diablo", "poclbm"}

	// Store results for summary
	type kernelResult struct {
		name    string
		correct int
		wrong   int
		errors  int
	}
	var results []kernelResult

	// Test each kernel
	for _, kernelType := range kernels {
		fmt.Fprintf(os.Stderr, "Testing kernel: %s\n", kernelType)

		correct := 0
		wrong := 0
		errors := 0

		for testNum := 0; testNum < 10; testNum++ {
			// Create a random event for each test
			testEvent := createRealisticBenchmarkEvent()

			// Test the kernel
			valid, nonce, err := testSingleKernel(selectedDevice, &testEvent, difficulty, kernelType)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Test %d: ERROR - %v\n", testNum+1, err)
				errors++
				continue
			}

			if valid {
				fmt.Fprintf(os.Stderr, "  Test %d: CORRECT (nonce: %d)\n", testNum+1, nonce)
				correct++
			} else {
				fmt.Fprintf(os.Stderr, "  Test %d: WRONG (nonce: %d failed validation)\n", testNum+1, nonce)
				wrong++
			}
		}

		fmt.Fprintf(os.Stderr, "\nKernel %s results:\n", kernelType)
		fmt.Fprintf(os.Stderr, "  Correct: %d/10\n", correct)
		fmt.Fprintf(os.Stderr, "  Wrong: %d/10\n", wrong)
		fmt.Fprintf(os.Stderr, "  Errors: %d/10\n", errors)
		fmt.Fprintf(os.Stderr, "\n")

		// Store results for summary
		results = append(results, kernelResult{
			name:    kernelType,
			correct: correct,
			wrong:   wrong,
			errors:  errors,
		})
	}

	// Print summary
	fmt.Fprintf(os.Stderr, "=== Summary ===\n")
	fmt.Fprintf(os.Stderr, "%-12s %8s %8s %8s\n", "Kernel", "Correct", "Wrong", "Errors")
	fmt.Fprintf(os.Stderr, "%-12s %8s %8s %8s\n", "------", "-------", "-----", "------")
	for _, r := range results {
		fmt.Fprintf(os.Stderr, "%-12s %8d %8d %8d\n", r.name, r.correct, r.wrong, r.errors)
	}
	fmt.Fprintf(os.Stderr, "\n")
}

// benchmarkBatchSizeSafe runs a benchmark for a specific batch size
// Returns the nonce rate in nonces per second and any error encountered
func benchmarkBatchSizeSafe(device *cl.Device, event *nostr.Event, difficulty int, batchSize int, kernelType string) (float64, error) {
	// Create context
	context, err := cl.CreateContext([]*cl.Device{device})
	if err != nil {
		return 0, fmt.Errorf("failed to create context: %v", err)
	}
	defer context.Release()

	// Create command queue
	queue, err := context.CreateCommandQueue(device, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to create command queue: %v", err)
	}
	defer queue.Release()

	// Get kernel source
	kernelSource, kernelName, err := getKernelSource(kernelType, device)
	if err != nil {
		return 0, err
	}
	// Show actual kernel selected (in case auto was used)
	actualKernel := kernelType
	if kernelType == "auto" {
		actualKernel = selectKernelForDevice(device)
	}
	vlog("Loading kernel: %s (function: %s)", actualKernel, kernelName)

	// Create program
	program, err := context.CreateProgramWithSource([]string{kernelSource})
	if err != nil {
		return 0, fmt.Errorf("failed to create program: %v", err)
	}
	defer program.Release()

	// Build program
	err = program.BuildProgram(nil, "")
	if err != nil {
		return 0, fmt.Errorf("failed to build program: %v", err)
	}

	// Create kernel
	kernel, err := program.CreateKernel(kernelName)
	if err != nil {
		return 0, fmt.Errorf("failed to create kernel: %v", err)
	}
	defer kernel.Release()

	// Prepare event with placeholder nonce
	testEvent := *event
	noncePlaceholder := "0000000000" // 10 digits
	testEvent.Tags = append(testEvent.Tags, nostr.Tag{"nonce", noncePlaceholder, strconv.Itoa(difficulty)})

	// Serialize event
	serialized := testEvent.Serialize()
	serializedLength := len(serialized)

	// Find nonce position
	noncePlaceholderBytes := []byte(noncePlaceholder)
	nonceOffset := bytes.Index(serialized, noncePlaceholderBytes)
	if nonceOffset == -1 {
		return 0, fmt.Errorf("could not find nonce placeholder in serialized event")
	}

	// Create input buffer
	inputBuffer, err := context.CreateEmptyBuffer(cl.MemReadOnly, serializedLength)
	if err != nil {
		return 0, fmt.Errorf("failed to create input buffer: %v", err)
	}
	defer inputBuffer.Release()

	// Write serialized event to buffer
	_, err = queue.EnqueueWriteBuffer(inputBuffer, true, 0, serializedLength, unsafe.Pointer(&serialized[0]), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to write input buffer: %v", err)
	}

	// Create results buffer
	resultSize := 4 // int32
	resultsBufferSize := batchSize * resultSize
	maxResultsBufferSize := 100 * 1024 * 1024
	if resultsBufferSize > maxResultsBufferSize {
		resultsBufferSize = maxResultsBufferSize
		batchSize = resultsBufferSize / resultSize
	}

	resultsBuffer, err := context.CreateEmptyBuffer(cl.MemWriteOnly, resultsBufferSize)
	if err != nil {
		return 0, fmt.Errorf("failed to create results buffer: %v", err)
	}
	defer resultsBuffer.Release()

	// Set kernel arguments (will be reused)
	err = kernel.SetArgBuffer(0, inputBuffer)
	if err != nil {
		return 0, fmt.Errorf("failed to set kernel arg 0: %v", err)
	}

	err = kernel.SetArgInt32(1, int32(serializedLength))
	if err != nil {
		return 0, fmt.Errorf("failed to set kernel arg 1: %v", err)
	}

	err = kernel.SetArgInt32(2, int32(nonceOffset))
	if err != nil {
		return 0, fmt.Errorf("failed to set kernel arg 2: %v", err)
	}

	err = kernel.SetArgInt32(3, int32(difficulty))
	if err != nil {
		return 0, fmt.Errorf("failed to set kernel arg 3: %v", err)
	}

	err = kernel.SetArgBuffer(6, resultsBuffer)
	if err != nil {
		return 0, fmt.Errorf("failed to set kernel arg 6: %v", err)
	}

	err = kernel.SetArgInt32(7, int32(10)) // 10 digits
	if err != nil {
		return 0, fmt.Errorf("failed to set kernel arg 7: %v", err)
	}

	// Benchmark for at least 5 seconds
	benchmarkDuration := 5 * time.Second
	startTime := time.Now()
	totalTested := int64(0)
	currentNonce := int64(1000000000) // Start at 10 digits

	results := make([]byte, resultsBufferSize)

	for time.Since(startTime) < benchmarkDuration {
		// Set nonce arguments
		baseNonceLow := uint32(currentNonce & 0xFFFFFFFF)
		baseNonceHigh := uint32((currentNonce >> 32) & 0xFFFFFFFF)
		err = kernel.SetArgInt32(4, int32(baseNonceLow))
		if err != nil {
			return 0, fmt.Errorf("failed to set kernel arg 4: %v", err)
		}

		err = kernel.SetArgInt32(5, int32(baseNonceHigh))
		if err != nil {
			return 0, fmt.Errorf("failed to set kernel arg 5: %v", err)
		}

		// Execute kernel
		remaining := batchSize
		globalSize := []int{remaining}
		_, err = queue.EnqueueNDRangeKernel(kernel, nil, globalSize, nil, nil)
		if err != nil {
			return 0, fmt.Errorf("failed to enqueue kernel: %v", err)
		}

		// Read results
		readSize := remaining * resultSize
		if readSize > resultsBufferSize {
			readSize = resultsBufferSize
			remaining = resultsBufferSize / resultSize
		}
		_, err = queue.EnqueueReadBuffer(resultsBuffer, true, 0, readSize, unsafe.Pointer(&results[0]), nil)
		if err != nil {
			return 0, fmt.Errorf("failed to read results buffer: %v", err)
		}

		totalTested += int64(remaining)
		currentNonce += int64(remaining)

		// Check if we should continue
		if time.Since(startTime) >= benchmarkDuration {
			break
		}
	}

	elapsed := time.Since(startTime)
	rate := float64(totalTested) / elapsed.Seconds()
	return rate, nil
}

func main() {
	// Parse CLI arguments
	difficulty := flag.Int("difficulty", 16, "Number of leading zero bits required (NIP-13)")
	batchSizePower := flag.Int("batch-size", -1, "Batch size as power of 10 (4=10000, 5=100000, etc.). -1 for auto-detect")
	listDevices := flag.Bool("list-devices", false, "List available OpenCL devices and exit")
	listDevicesShort := flag.Bool("l", false, "List available OpenCL devices and exit (short)")
	deviceIndex := flag.Int("device", -1, "Select device by index from list (use -list-devices to see available devices)")
	deviceIndexShort := flag.Int("d", -1, "Select device by index from list (short)")
	benchmark := flag.Bool("benchmark", false, "Benchmark different batch sizes to find optimal value")
	testKernels := flag.Bool("test-kernels", false, "Test all kernels with random events to verify correctness")
	kernelType := flag.String("kernel", "auto", "Kernel implementation to use: 'auto' (select based on device), 'default' (our implementation), 'ckolivas' (sgminer), 'phatk', 'diakgcn', 'diablo', or 'poclbm' (bfgminer)")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose logging")
	flag.Parse()

	// Handle short flags
	if *listDevicesShort {
		*listDevices = true
	}
	if *deviceIndexShort != -1 {
		*deviceIndex = *deviceIndexShort
	}

	if *difficulty < 0 || *difficulty > 256 {
		log.Fatalf("Difficulty must be between 0 and 256, got %d", *difficulty)
	}

	if *batchSizePower < -1 || *batchSizePower > 10 {
		log.Fatalf("Batch size power must be between -1 (auto) and 10 (10000000000), got %d", *batchSizePower)
	}

	// Get platforms
	platforms, err := cl.GetPlatforms()
	if err != nil {
		log.Fatalf("Failed to get platforms: %v", err)
	}

	if len(platforms) == 0 {
		log.Fatal("No OpenCL platforms found")
	}

	// List devices and exit if requested
	if *listDevices {
		listAllDevices()
		os.Exit(0)
	}

	// Run benchmark if requested
	if *benchmark {
		runBenchmark(*difficulty, *deviceIndex, *kernelType)
		os.Exit(0)
	}

	// Test all kernels if requested
	if *testKernels {
		testAllKernels(*difficulty, *deviceIndex)
		os.Exit(0)
	}

	// Collect all devices from all platforms
	var allDevices []*cl.Device
	var devicePlatforms []int
	for platformIdx, platform := range platforms {
		devices, err := platform.GetDevices(cl.DeviceTypeAll)
		if err != nil {
			vlog("Warning: Failed to get devices from platform %d: %v", platformIdx, err)
			continue
		}
		for _, device := range devices {
			allDevices = append(allDevices, device)
			devicePlatforms = append(devicePlatforms, platformIdx)
		}
	}

	if len(allDevices) == 0 {
		log.Fatal("No OpenCL devices found")
	}

	// Select device
	var selectedDevice *cl.Device
	if *deviceIndex >= 0 {
		if *deviceIndex >= len(allDevices) {
			log.Fatalf("Device index %d is out of range. Use -list-devices to see available devices (0-%d)",
				*deviceIndex, len(allDevices)-1)
		}
		selectedDevice = allDevices[*deviceIndex]
		deviceName := selectedDevice.Name()
		vlog("Selected device [%d]: %s", *deviceIndex, deviceName)
	} else {
		// Default: prefer GPU devices, then use first available
		selectedDevice = nil
		for i, device := range allDevices {
			deviceType := device.Type()
			if (deviceType & cl.DeviceTypeGPU) != 0 {
				selectedDevice = device
				deviceName := device.Name()
				vlog("Auto-selected GPU device [%d]: %s", i, deviceName)
				break
			}
		}
		if selectedDevice == nil {
			// No GPU found, use first device
			selectedDevice = allDevices[0]
			deviceName := selectedDevice.Name()
			vlog("Auto-selected device [0]: %s", deviceName)
		}
	}

	devices := []*cl.Device{selectedDevice}

	// Auto-detect batch size if not specified
	var batchSize int
	if *batchSizePower == -1 {
		// Auto-detect based on device capabilities
		device := devices[0]
		maxComputeUnits := device.MaxComputeUnits()
		maxWorkGroupSize := device.MaxWorkGroupSize()
		globalMemSize := device.GlobalMemSize()

		// Estimate optimal batch size based on device capabilities
		// Use compute units and work group size as indicators
		estimatedCapacity := maxComputeUnits * maxWorkGroupSize

		// Memory check: each work item needs ~2KB private + 4 bytes output
		// Use 1% of global memory as a safe limit
		memoryLimit := int(globalMemSize / (2048 + 4) / 100)

		// Choose batch size based on capacity
		// Conservative estimate: use 10-50% of estimated capacity
		optimalSize := estimatedCapacity / 10
		if optimalSize > memoryLimit {
			optimalSize = memoryLimit
		}

		// Round down to nearest power of 10
		// Be conservative for CPU devices, more aggressive for GPUs
		deviceType := device.Type()
		isGPU := (deviceType & cl.DeviceTypeGPU) != 0

		if isGPU {
			// GPU: can handle larger batches
			if optimalSize >= 1000000 {
				*batchSizePower = 6 // 1,000,000
			} else if optimalSize >= 100000 {
				*batchSizePower = 5 // 100,000
			} else if optimalSize >= 10000 {
				*batchSizePower = 4 // 10,000
			} else {
				*batchSizePower = 4 // Default to 10,000
			}
		} else {
			// CPU: be more conservative
			if optimalSize >= 100000 {
				*batchSizePower = 5 // 100,000
			} else if optimalSize >= 10000 {
				*batchSizePower = 4 // 10,000
			} else if optimalSize >= 1000 {
				*batchSizePower = 3 // 1,000
			} else {
				*batchSizePower = 4 // Default to 10,000 for safety
			}
		}

		vlog("  Device type: %s", deviceType.String())

		vlog("Auto-detected device capabilities:")
		vlog("  Compute units: %d", maxComputeUnits)
		vlog("  Max work group size: %d", maxWorkGroupSize)
		vlog("  Global memory: %d MB", globalMemSize/(1024*1024))
		vlog("  Estimated capacity: %d work items", estimatedCapacity)
		vlog("  Selected batch size: 10^%d = %d", *batchSizePower, int(math.Pow(10, float64(*batchSizePower))))
	}

	batchSize = int(math.Pow(10, float64(*batchSizePower)))

	// Additional safety: limit batch size based on max work group size
	// Some OpenCL implementations have issues with very large global sizes
	maxWorkGroupSize := devices[0].MaxWorkGroupSize()
	if batchSize > maxWorkGroupSize*100 {
		// Limit to 100x the work group size as a safety measure
		originalBatchSize := batchSize
		batchSize = maxWorkGroupSize * 100
		// Round down to nearest power of 10
		batchSizePowerAdjusted := int(math.Floor(math.Log10(float64(batchSize))))
		batchSize = int(math.Pow(10, float64(batchSizePowerAdjusted)))
		if batchSize != originalBatchSize {
			vlog("Warning: Adjusted batch size from %d to %d based on work group size limit", originalBatchSize, batchSize)
		}
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

	// Get kernel source
	kernelSource, kernelName, err := getKernelSource(*kernelType, selectedDevice)
	if err != nil {
		log.Fatalf("Failed to get kernel source: %v", err)
	}
	// Show actual kernel selected (in case auto was used)
	actualKernel := *kernelType
	if *kernelType == "auto" {
		actualKernel = selectKernelForDevice(selectedDevice)
		vlog("Auto-selected kernel: %s (function: %s) for device: %s", actualKernel, kernelName, selectedDevice.Name())
	} else {
		vlog("Using kernel: %s (function: %s)", actualKernel, kernelName)
	}

	// Create program
	program, err := context.CreateProgramWithSource([]string{kernelSource})
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
	kernel, err := program.CreateKernel(kernelName)
	if err != nil {
		log.Fatalf("Failed to create kernel: %v", err)
	}
	defer kernel.Release()

	// Read JSON event from stdin
	jsonBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read from stdin: %v", err)
	}

	if len(jsonBytes) == 0 {
		log.Fatal("No input provided")
	}

	// Parse the JSON event using go-nostr library
	var event nostr.Event
	if err := json.Unmarshal(jsonBytes, &event); err != nil {
		log.Fatalf("Failed to parse JSON event: %v", err)
	}

	// Calculate maximum number of digits needed for nonce based on difficulty
	// Expected attempts = 2^difficulty, we want 2 orders of magnitude more
	expectedAttempts := math.Pow(2, float64(*difficulty))
	maxRequiredDigits := int(math.Ceil(math.Log10(expectedAttempts))) + 2
	if maxRequiredDigits < 10 {
		maxRequiredDigits = 10 // Minimum 10 digits for compatibility
	}

	// Calculate minimum digits needed to hold at least one batch
	// We need at least enough digits to represent batchSize
	minRequiredDigits := int(math.Ceil(math.Log10(float64(batchSize)))) + 1
	if minRequiredDigits < 5 {
		minRequiredDigits = 5 // Minimum 5 digits
	}

	vlog("Difficulty: %d, Nonce digits: %d-%d (dynamic sizing)", *difficulty, minRequiredDigits, maxRequiredDigits)

	// Remove any existing nonce tag to avoid duplicates
	filteredTags := make(nostr.Tags, 0, len(event.Tags))
	for _, tag := range event.Tags {
		if len(tag) > 0 && tag[0] != "nonce" {
			filteredTags = append(filteredTags, tag)
		}
	}
	event.Tags = filteredTags

	// We'll dynamically add the nonce tag and find its position
	// Start with minimum digits for the first batch
	currentDigits := minRequiredDigits

	vlog("Mining with difficulty %d (leading zero bits)", *difficulty)
	vlog("Batch size: %d nonces", batchSize)

	// Results buffer: index (int32, 4 bytes) per work item
	// -1 means not found, >= 0 means valid nonce found at that index
	resultSize := 4 // int32
	resultsBufferSize := batchSize * resultSize

	// Safety check: limit results buffer to reasonable size (100MB)
	maxResultsBufferSize := 100 * 1024 * 1024
	if resultsBufferSize > maxResultsBufferSize {
		vlog("Warning: Batch size %d would require %d MB results buffer, limiting to %d MB",
			batchSize, resultsBufferSize/(1024*1024), maxResultsBufferSize/(1024*1024))
		batchSize = maxResultsBufferSize / resultSize
		resultsBufferSize = batchSize * resultSize
		vlog("Adjusted batch size to %d", batchSize)
	}

	resultsBuffer, err := context.CreateEmptyBuffer(cl.MemWriteOnly, resultsBufferSize)
	if err != nil {
		log.Fatalf("Failed to create results buffer: %v", err)
	}
	defer resultsBuffer.Release()

	// Mining loop with dynamic nonce sizing
	found := false
	var foundNonce uint64
	var foundEventID []byte
	var currentNonce int64
	var nonceOffset int
	var serializedLength int
	var serialized []byte
	var inputBuffer *cl.MemObject

	// Progress tracking
	startTime := time.Now()
	totalTested := int64(0)
	lastProgressUpdate := time.Now()

	for currentDigits <= maxRequiredDigits && !found {
		// Calculate nonce range for current digit size
		baseNonceValue := int64(math.Pow(10, float64(currentDigits-1)))
		maxNonceValue := int64(math.Pow(10, float64(currentDigits))) - 1

		// Generate placeholder nonce with current digits (zero-padded)
		noncePlaceholder := fmt.Sprintf("%0*d", currentDigits, baseNonceValue)

		// Add/update nonce tag with current placeholder
		// Remove existing nonce tag first
		filteredTags := make(nostr.Tags, 0, len(event.Tags))
		for _, tag := range event.Tags {
			if len(tag) > 0 && tag[0] != "nonce" {
				filteredTags = append(filteredTags, tag)
			}
		}
		event.Tags = filteredTags
		event.Tags = append(event.Tags, nostr.Tag{"nonce", noncePlaceholder, strconv.Itoa(*difficulty)})

		// Serialize event with current placeholder
		serialized = event.Serialize()
		serializedLength = len(serialized)

		// Find nonce position in serialized string
		noncePlaceholderBytes := []byte(noncePlaceholder)
		nonceOffset = bytes.Index(serialized, noncePlaceholderBytes)
		if nonceOffset == -1 {
			log.Fatalf("Could not find nonce placeholder in serialized event (digits: %d)", currentDigits)
		}

		// Create/update input buffer for base serialized event
		if inputBuffer != nil {
			inputBuffer.Release()
		}
		inputBuffer, err = context.CreateEmptyBuffer(cl.MemReadOnly, serializedLength)
		if err != nil {
			log.Fatalf("Failed to create input buffer: %v", err)
		}

		// Write base serialized event to buffer
		_, err = queue.EnqueueWriteBuffer(inputBuffer, true, 0, serializedLength, unsafe.Pointer(&serialized[0]), nil)
		if err != nil {
			log.Fatalf("Failed to write input buffer: %v", err)
		}

		vlog("Trying %d-digit nonces: %d to %d", currentDigits, baseNonceValue, maxNonceValue)

		// Start from base nonce for this digit size
		currentNonce = baseNonceValue

		// Process batches for this digit size
		for currentNonce <= maxNonceValue && !found {
			// Calculate how many nonces to test in this batch
			remaining := int(maxNonceValue - currentNonce + 1)
			if remaining > batchSize {
				remaining = batchSize
			}

			// Set kernel arguments
			err = kernel.SetArgBuffer(0, inputBuffer)
			if err != nil {
				log.Fatalf("Failed to set kernel arg 0: %v", err)
			}

			err = kernel.SetArgInt32(1, int32(serializedLength))
			if err != nil {
				log.Fatalf("Failed to set kernel arg 1: %v", err)
			}

			err = kernel.SetArgInt32(2, int32(nonceOffset))
			if err != nil {
				log.Fatalf("Failed to set kernel arg 2: %v", err)
			}

			err = kernel.SetArgInt32(3, int32(*difficulty))
			if err != nil {
				log.Fatalf("Failed to set kernel arg 3: %v", err)
			}

			// Pass nonce as two 32-bit values to avoid overflow
			baseNonceLow := uint32(currentNonce & 0xFFFFFFFF)
			baseNonceHigh := uint32((currentNonce >> 32) & 0xFFFFFFFF)
			err = kernel.SetArgInt32(4, int32(baseNonceLow))
			if err != nil {
				log.Fatalf("Failed to set kernel arg 4: %v", err)
			}

			err = kernel.SetArgInt32(5, int32(baseNonceHigh))
			if err != nil {
				log.Fatalf("Failed to set kernel arg 5: %v", err)
			}

			err = kernel.SetArgBuffer(6, resultsBuffer)
			if err != nil {
				log.Fatalf("Failed to set kernel arg 6 (results buffer): %v", err)
			}

			err = kernel.SetArgInt32(7, int32(currentDigits))
			if err != nil {
				log.Fatalf("Failed to set kernel arg 7: %v", err)
			}

			// Execute kernel
			globalSize := []int{remaining}
			// Let OpenCL choose optimal local work group size
			_, err = queue.EnqueueNDRangeKernel(kernel, nil, globalSize, nil, nil)
			if err != nil {
				log.Fatalf("Failed to enqueue kernel: %v", err)
			}

			// Read results (limit to actual buffer size)
			readSize := remaining * resultSize
			if readSize > resultsBufferSize {
				readSize = resultsBufferSize
				remaining = resultsBufferSize / resultSize
			}
			results := make([]byte, readSize)
			_, err = queue.EnqueueReadBuffer(resultsBuffer, true, 0, readSize, unsafe.Pointer(&results[0]), nil)
			if err != nil {
				log.Fatalf("Failed to read results buffer: %v", err)
			}

			// Check results - read as int32 array
			resultIndices := (*[1 << 28]int32)(unsafe.Pointer(&results[0]))[:remaining:remaining]
			for i := 0; i < remaining; i++ {
				index := resultIndices[i]
				if index >= 0 {
					// Found candidate nonce! Calculate nonce from index
					candidateNonce := uint64(currentNonce) + uint64(index)

					// Validate this candidate by recalculating hash on CPU
					if validateNonce(candidateNonce, &event, *difficulty, currentDigits) {
						// Valid nonce found! Recalculate event ID for final output
						// Create a deep copy of the event
						testEvent := event
						nonceStr := fmt.Sprintf("%0*d", currentDigits, candidateNonce)

						// Deep copy tags to avoid modifying the original
						testEvent.Tags = make(nostr.Tags, len(event.Tags))
						for j, tag := range event.Tags {
							testEvent.Tags[j] = make(nostr.Tag, len(tag))
							copy(testEvent.Tags[j], tag)
						}

						// Remove old nonce tags and add new one
						filteredTags := make(nostr.Tags, 0, len(testEvent.Tags))
						for _, tag := range testEvent.Tags {
							if len(tag) == 0 || tag[0] != "nonce" {
								filteredTags = append(filteredTags, tag)
							}
						}
						testEvent.Tags = append(filteredTags, nostr.Tag{"nonce", nonceStr, strconv.Itoa(*difficulty)})

						// Recalculate event ID
						eventIDHex := testEvent.GetID()
						foundEventID, err = hex.DecodeString(eventIDHex)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Error decoding event ID: %v\n", err)
							continue
						}

						foundNonce = candidateNonce
						found = true
						break
					} else {
						// Invalid result, continue mining
						// Error already logged to stderr by validateNonce
						continue
					}
				}
			}

			if !found {
				currentNonce += int64(remaining)
				totalTested += int64(remaining)

				// Update progress bar every 100ms
				now := time.Now()
				if now.Sub(lastProgressUpdate) >= 100*time.Millisecond {
					updateProgressBar(currentNonce-1, currentDigits, totalTested, startTime, *difficulty)
					lastProgressUpdate = now
				}

				if currentNonce%1000000 == 0 {
					vlog("Tested up to nonce %d (%d digits)...", currentNonce-1, currentDigits)
				}
			} else {
				// Clear progress bar when found
				fmt.Fprintf(os.Stderr, "\r%s\r", "                                                                                ")
			}
		}

		// If we've exhausted this digit size, move to next
		if !found && currentNonce > maxNonceValue {
			vlog("Exhausted %d-digit nonces, moving to %d digits", currentDigits, currentDigits+1)
			currentDigits++
		}
	}

	// Clear progress bar line
	fmt.Fprintf(os.Stderr, "\r%s\r", "                                                                                ")

	if inputBuffer != nil {
		inputBuffer.Release()
	}

	if !found {
		log.Fatalf("Could not find valid nonce up to %d digits (max for difficulty %d)", maxRequiredDigits, *difficulty)
	}

	// Update event with found nonce (format with correct number of digits)
	// Calculate how many digits the found nonce actually needs
	foundNonceDigits := int(math.Ceil(math.Log10(float64(foundNonce) + 1)))
	if foundNonceDigits < minRequiredDigits {
		foundNonceDigits = minRequiredDigits
	}
	nonceStr := fmt.Sprintf("%0*d", foundNonceDigits, foundNonce)
	// Find and update nonce tag
	for i, tag := range event.Tags {
		if len(tag) > 0 && tag[0] == "nonce" {
			event.Tags[i] = nostr.Tag{"nonce", nonceStr, strconv.Itoa(*difficulty)}
			break
		}
	}

	// Set the event ID
	eventIDHex := ""
	for _, b := range foundEventID {
		eventIDHex += fmt.Sprintf("%02x", b)
	}
	event.ID = eventIDHex

	// Final validation (should always pass since we validated in the loop)
	// This is just a sanity check
	expectedID := event.GetID()
	if expectedID != eventIDHex {
		log.Fatalf("Internal error: Event ID mismatch after validation! Expected: %s, Got: %s", expectedID, eventIDHex)
	}

	// Log validation success
	actualDifficulty := nip13.Difficulty(eventIDHex)
	vlog("Validation successful: Event ID has %d leading zero bits (required: %d)", actualDifficulty, *difficulty)

	// Output final event as JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		log.Fatalf("Failed to marshal final event: %v", err)
	}

	fmt.Println(string(eventJSON))
}
