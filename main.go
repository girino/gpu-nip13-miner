package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"unsafe"

	cl "github.com/jgillich/go-opencl/cl"
	"github.com/nbd-wtf/go-nostr"
)

func main() {
	// Parse CLI arguments
	difficulty := flag.Int("difficulty", 16, "Number of leading zero bits required (NIP-13)")
	flag.Parse()

	if *difficulty < 0 || *difficulty > 256 {
		log.Fatalf("Difficulty must be between 0 and 256, got %d", *difficulty)
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

	// Remove any existing nonce tag to avoid duplicates
	filteredTags := make(nostr.Tags, 0, len(event.Tags))
	for _, tag := range event.Tags {
		if len(tag) > 0 && tag[0] != "nonce" {
			filteredTags = append(filteredTags, tag)
		}
	}
	event.Tags = filteredTags

	// Add nonce tag with placeholder (1000000000)
	noncePlaceholder := "1000000000"
	event.Tags = append(event.Tags, nostr.Tag{"nonce", noncePlaceholder, strconv.Itoa(*difficulty)})

	// Serialize event with placeholder nonce
	serialized := event.Serialize()

	// Find nonce position in serialized string
	noncePlaceholderBytes := []byte(noncePlaceholder)
	nonceOffset := bytes.Index(serialized, noncePlaceholderBytes)
	if nonceOffset == -1 {
		log.Fatal("Could not find nonce placeholder in serialized event")
	}

	serializedLength := len(serialized)
	baseNonce := 1000000000
	maxNonce := 9999999999
	batchSize := 10000 // Process 10k nonces per batch (adjust based on GPU capabilities)

	log.Printf("Mining with difficulty %d (leading zero bits)", *difficulty)
	log.Printf("Nonce range: %d to %d", baseNonce, maxNonce)
	log.Printf("Batch size: %d nonces", batchSize)

	// Create input buffer for base serialized event
	inputBuffer, err := context.CreateEmptyBuffer(cl.MemReadOnly, serializedLength)
	if err != nil {
		log.Fatalf("Failed to create input buffer: %v", err)
	}
	defer inputBuffer.Release()

	// Write base serialized event to buffer
	_, err = queue.EnqueueWriteBuffer(inputBuffer, true, 0, serializedLength, unsafe.Pointer(&serialized[0]), nil)
	if err != nil {
		log.Fatalf("Failed to write input buffer: %v", err)
	}

	// Results buffer: [found (1 byte), nonce (8 bytes), event_id (32 bytes)] per work item
	resultSize := 41 // 1 + 8 + 32
	resultsBuffer, err := context.CreateEmptyBuffer(cl.MemWriteOnly, batchSize*resultSize)
	if err != nil {
		log.Fatalf("Failed to create results buffer: %v", err)
	}
	defer resultsBuffer.Release()

	// Mining loop
	currentNonce := baseNonce
	found := false
	var foundNonce uint64
	var foundEventID []byte

	for currentNonce <= maxNonce && !found {
		// Calculate how many nonces to test in this batch
		remaining := maxNonce - currentNonce + 1
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

		// Read results
		results := make([]byte, remaining*resultSize)
		_, err = queue.EnqueueReadBuffer(resultsBuffer, true, 0, remaining*resultSize, unsafe.Pointer(&results[0]), nil)
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
			currentNonce += remaining
			if currentNonce%1000000 == 0 {
				log.Printf("Tested up to nonce %d...", currentNonce-1)
			}
		}
	}

	if !found {
		log.Fatal("Could not find valid nonce in range 1000000000-9999999999")
	}

	// Update event with found nonce
	nonceStr := strconv.FormatUint(foundNonce, 10)
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

	// Output final event as JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		log.Fatalf("Failed to marshal final event: %v", err)
	}

	fmt.Println(string(eventJSON))
}
