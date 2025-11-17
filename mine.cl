// NIP-13 Mining Kernel
// Mines nonces in parallel to find event IDs with required leading zero bits

#define ROTRIGHT(a,b) (((a) >> (b)) | ((a) << (32-(b))))

#define CH(x,y,z) (((x) & (y)) ^ (~(x) & (z)))
#define MAJ(x,y,z) (((x) & (y)) ^ ((x) & (z)) ^ ((y) & (z)))
#define EP0(x) (ROTRIGHT(x,2) ^ ROTRIGHT(x,13) ^ ROTRIGHT(x,22))
#define EP1(x) (ROTRIGHT(x,6) ^ ROTRIGHT(x,11) ^ ROTRIGHT(x,25))
#define SIG0(x) (ROTRIGHT(x,7) ^ ROTRIGHT(x,18) ^ ((x) >> 3))
#define SIG1(x) (ROTRIGHT(x,17) ^ ROTRIGHT(x,19) ^ ((x) >> 10))

// SHA256 constants
__constant uint k[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
    0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
    0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
    0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
    0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
};

// Process a single 512-bit block
void process_block(uchar block[64], uint h[8]) {
    // Convert block to 16 uint32 words (big-endian)
    uint w[64];
    for (int i = 0; i < 16; i++) {
        w[i] = ((uint)block[i * 4] << 24) |
               ((uint)block[i * 4 + 1] << 16) |
               ((uint)block[i * 4 + 2] << 8) |
               ((uint)block[i * 4 + 3]);
    }
    
    // Extend the 16 words into 64 words
    for (int i = 16; i < 64; i++) {
        w[i] = SIG1(w[i-2]) + w[i-7] + SIG0(w[i-15]) + w[i-16];
    }
    
    // Initialize working variables
    uint a = h[0];
    uint b = h[1];
    uint c = h[2];
    uint d = h[3];
    uint e = h[4];
    uint f = h[5];
    uint g = h[6];
    uint h_val = h[7];
    
    // Main loop
    for (int i = 0; i < 64; i++) {
        uint S1 = EP1(e);
        uint ch = CH(e, f, g);
        uint temp1 = h_val + S1 + ch + k[i] + w[i];
        uint S0 = EP0(a);
        uint maj = MAJ(a, b, c);
        uint temp2 = S0 + maj;
        
        h_val = g;
        g = f;
        f = e;
        e = d + temp1;
        d = c;
        c = b;
        b = a;
        a = temp1 + temp2;
    }
    
    // Add the compressed chunk to the current hash value
    h[0] += a;
    h[1] += b;
    h[2] += c;
    h[3] += d;
    h[4] += e;
    h[5] += f;
    h[6] += g;
    h[7] += h_val;
}

// Convert integer to N-digit decimal ASCII string (zero-padded)
void int_to_ascii(ulong n, uchar str[], int num_digits) {
    // Convert to N-digit string (digits from right to left)
    for (int i = num_digits - 1; i >= 0; i--) {
        str[i] = '0' + (n % 10);
        n /= 10;
    }
}

// Calculate SHA256 of input (works with any address space)
void sha256_generic(uchar* input, int input_length, uchar output[32]) {
    // SHA256 initial hash values
    uint h[8] = {
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
    };
    
    // Calculate number of complete 64-byte blocks
    int num_complete_blocks = input_length / 64;
    int remaining_bytes = input_length % 64;
    ulong original_bit_length = (ulong)input_length * 8;
    
    // Process all complete 64-byte blocks
    uchar block[64];
    for (int block_idx = 0; block_idx < num_complete_blocks; block_idx++) {
        // Copy 64 bytes from input
        for (int i = 0; i < 64; i++) {
            block[i] = input[block_idx * 64 + i];
        }
        process_block(block, h);
    }
    
    // Process the final block with padding
    // Clear the block
    for (int i = 0; i < 64; i++) {
        block[i] = 0;
    }
    
    // Copy remaining bytes
    for (int i = 0; i < remaining_bytes; i++) {
        block[i] = input[num_complete_blocks * 64 + i];
    }
    
    // Add padding: 0x80 byte
    block[remaining_bytes] = 0x80;
    
    // If remaining bytes + 1 (0x80) + 8 (length) > 64, we need an extra block
    if (remaining_bytes < 56) {
        // Length fits in this block - add length in bits (big-endian, 64 bits)
        for (int i = 0; i < 8; i++) {
            block[56 + i] = (uchar)((original_bit_length >> (56 - i * 8)) & 0xff);
        }
        process_block(block, h);
    } else {
        // Length doesn't fit - process this block and create another
        process_block(block, h);
        
        // Create final block with just the length
        for (int i = 0; i < 64; i++) {
            block[i] = 0;
        }
        for (int i = 0; i < 8; i++) {
            block[56 + i] = (uchar)((original_bit_length >> (56 - i * 8)) & 0xff);
        }
        process_block(block, h);
    }
    
    // Write output (32 bytes = 256 bits)
    for (int i = 0; i < 8; i++) {
        output[i * 4] = (uchar)((h[i] >> 24) & 0xff);
        output[i * 4 + 1] = (uchar)((h[i] >> 16) & 0xff);
        output[i * 4 + 2] = (uchar)((h[i] >> 8) & 0xff);
        output[i * 4 + 3] = (uchar)(h[i] & 0xff);
    }
}

// Count leading zero bits in a byte array
int count_leading_zero_bits(uchar hash[32]) {
    int count = 0;
    for (int i = 0; i < 32; i++) {
        uchar b = hash[i];
        if (b == 0) {
            count += 8;
        } else {
            // Count leading zeros in this byte
            for (int j = 7; j >= 0; j--) {
                if ((b >> j) & 1) {
                    break;
                }
                count++;
            }
            break;
        }
    }
    return count;
}

__kernel void mine_nonce(
    __global uchar* base_serialized,  // Base serialized event with placeholder nonce
    int serialized_length,             // Length of serialized event
    int nonce_offset,                  // Byte position where nonce starts in string
    int difficulty,                    // Required leading zero bits
    int base_nonce,                    // Starting nonce value
    __global uchar* results,           // Output: [found (1 byte), nonce (8 bytes), event_id (32 bytes)]
    int num_digits                     // Number of digits for nonce (e.g., 10, 20, etc.)
) {
    int global_id = get_global_id(0);
    ulong nonce = (ulong)base_nonce + (ulong)global_id;
    
    // Calculate maximum nonce value (10^num_digits - 1)
    ulong max_nonce = 1;
    for (int i = 0; i < num_digits; i++) {
        max_nonce *= 10;
    }
    max_nonce -= 1;
    
    // Check if nonce exceeds maximum
    if (nonce > max_nonce) {
        results[global_id * 41] = 0; // Not found
        return;
    }
    
    // Use private memory (stack) for work item's serialized string
    // Most events are < 1KB, so this should fit in private memory
    uchar serialized_copy[2048]; // Max 2KB per work item
    if (serialized_length > 2048) {
        results[global_id * 41] = 0; // Event too large
        return;
    }
    
    // Copy base serialized string to private buffer
    for (int i = 0; i < serialized_length; i++) {
        serialized_copy[i] = base_serialized[i];
    }
    
    // Convert nonce to N-digit ASCII string (zero-padded)
    // Use a fixed-size array that can handle up to 22 digits (for difficulty 64)
    uchar nonce_str[22];
    if (num_digits > 22) {
        results[global_id * 41] = 0; // Too many digits
        return;
    }
    int_to_ascii(nonce, nonce_str, num_digits);
    
    // Replace nonce in the serialized string
    for (int i = 0; i < num_digits; i++) {
        serialized_copy[nonce_offset + i] = nonce_str[i];
    }
    
    // Calculate SHA256
    uchar hash[32];
    sha256_generic(serialized_copy, serialized_length, hash);
    
    // Check if difficulty requirement is met
    int leading_zeros = count_leading_zero_bits(hash);
    
    if (leading_zeros >= difficulty) {
        // Found valid nonce!
        results[global_id * 41] = 1; // Found flag
        // Store nonce (8 bytes, big-endian)
        ulong nonce_be = nonce;
        for (int i = 0; i < 8; i++) {
            results[global_id * 41 + 1 + i] = (uchar)((nonce_be >> (56 - i * 8)) & 0xff);
        }
        // Store event ID (32 bytes)
        for (int i = 0; i < 32; i++) {
            results[global_id * 41 + 9 + i] = hash[i];
        }
    } else {
        results[global_id * 41] = 0; // Not found
    }
}

