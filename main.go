package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strconv"
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

func main() {
	// Parse CLI arguments
	difficulty := flag.Int("difficulty", 16, "Number of leading zero bits required (NIP-13)")
	batchSizePower := flag.Int("batch-size", -1, "Batch size as power of 10 (4=10000, 5=100000, etc.). -1 for auto-detect")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose logging")
	flag.Parse()

	if *difficulty < 0 || *difficulty > 256 {
		log.Fatalf("Difficulty must be between 0 and 256, got %d", *difficulty)
	}

	if *batchSizePower < -1 || *batchSizePower > 6 {
		log.Fatalf("Batch size power must be between -1 (auto) and 6 (1000000), got %d", *batchSizePower)
	}

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

		// Memory check: each work item needs ~2KB private + 41 bytes output
		// Use 1% of global memory as a safe limit
		memoryLimit := int(globalMemSize / (2048 + 41) / 100)

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

	// Read mining kernel source from file
	kernelSource, err := os.ReadFile("mine.cl")
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
	kernel, err := program.CreateKernel("mine_nonce")
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

	// Results buffer: [found (1 byte), nonce (8 bytes), event_id (32 bytes)] per work item
	resultSize := 41 // 1 + 8 + 32
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

			err = kernel.SetArgInt32(4, int32(currentNonce))
			if err != nil {
				log.Fatalf("Failed to set kernel arg 4: %v", err)
			}

			err = kernel.SetArgInt32(6, int32(currentDigits))
			if err != nil {
				log.Fatalf("Failed to set kernel arg 6: %v", err)
			}

			err = kernel.SetArgBuffer(5, resultsBuffer)
			if err != nil {
				log.Fatalf("Failed to set kernel arg 5: %v", err)
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

			// Check results
			for i := 0; i < remaining; i++ {
				if results[i*resultSize] == 1 {
					// Found valid nonce!
					foundNonceBytes := results[i*resultSize+1 : i*resultSize+9]
					foundNonce = binary.BigEndian.Uint64(foundNonceBytes)
					foundEventID = make([]byte, 32)
					copy(foundEventID, results[i*resultSize+9:i*resultSize+41])
					found = true
					break
				}
			}

			if !found {
				currentNonce += int64(remaining)
				if currentNonce%1000000 == 0 {
					vlog("Tested up to nonce %d (%d digits)...", currentNonce-1, currentDigits)
				}
			}
		}

		// If we've exhausted this digit size, move to next
		if !found && currentNonce > maxNonceValue {
			vlog("Exhausted %d-digit nonces, moving to %d digits", currentDigits, currentDigits+1)
			currentDigits++
		}
	}

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

	// Validate the event ID using NIP-13
	// First, verify the ID matches what we calculated by re-serializing
	expectedID := event.GetID()
	if expectedID != eventIDHex {
		log.Fatalf("Event ID mismatch! Expected: %s, Got: %s", expectedID, eventIDHex)
	}

	// Validate difficulty using NIP-13 Check function
	if err := nip13.Check(eventIDHex, *difficulty); err != nil {
		log.Fatalf("NIP-13 validation failed: %v", err)
	}

	// Additional validation: check committed difficulty matches
	committedDiff := nip13.CommittedDifficulty(&event)
	if committedDiff != *difficulty {
		log.Fatalf("Committed difficulty mismatch! Expected: %d, Got: %d", *difficulty, committedDiff)
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
