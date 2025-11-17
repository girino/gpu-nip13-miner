/*-
 * Copyright 2009 Colin Percival, 2011 ArtForz, 2011 pooler, 2012 mtrlt,
 * 2012-2013 Con Kolivas.
 * 
 * MODIFIED: This file has been adapted for NIP-13 mining from the original
 * bfgminer poclbm kernel. The original implementation was for Bitcoin SHA256d
 * mining and has been modified to work with NIP-13 proof-of-work.
 * 
 * Original source: https://github.com/luke-jr/bfgminer/blob/bfgminer/opencl/poclbm.cl
 * 
 * Changes made:
 * - Adapted SHA256 implementation for NIP-13 mining
 * - Changed kernel interface to mine_nonce() for NIP-13 compatibility
 * - Based on ckolivas adaptation pattern, using poclbm naming
 */

// SHA256 constants from bfgminer_poclbm
__constant uint K[] = {
  0x428a2f98U, 0x71374491U, 0xb5c0fbcfU, 0xe9b5dba5U,
  0x3956c25bU, 0x59f111f1U, 0x923f82a4U, 0xab1c5ed5U,
  0xd807aa98U, 0x12835b01U, 0x243185beU, 0x550c7dc3U,
  0x72be5d74U, 0x80deb1feU, 0x9bdc06a7U, 0xc19bf174U,
  0xe49b69c1U, 0xefbe4786U, 0x0fc19dc6U, 0x240ca1ccU,
  0x2de92c6fU, 0x4a7484aaU, 0x5cb0a9dcU, 0x76f988daU,
  0x983e5152U, 0xa831c66dU, 0xb00327c8U, 0xbf597fc7U,
  0xc6e00bf3U, 0xd5a79147U, 0x06ca6351U, 0x14292967U,
  0x27b70a85U, 0x2e1b2138U, 0x4d2c6dfcU, 0x53380d13U,
  0x650a7354U, 0x766a0abbU, 0x81c2c92eU, 0x92722c85U,
  0xa2bfe8a1U, 0xa81a664bU, 0xc24b8b70U, 0xc76c51a3U,
  0xd192e819U, 0xd6990624U, 0xf40e3585U, 0x106aa070U,
  0x19a4c116U, 0x1e376c08U, 0x2748774cU, 0x34b0bcb5U,
  0x391c0cb3U, 0x4ed8aa4aU, 0x5b9cca4fU, 0x682e6ff3U,
  0x748f82eeU, 0x78a5636fU, 0x84c87814U, 0x8cc70208U,
  0x90befffaU, 0xa4506cebU, 0xbef9a3f7U, 0xc67178f2U
};

#define rotl(x,y) rotate(x,y)
#define Ch(x,y,z) bitselect(z,y,x)
#define Maj(x,y,z) Ch((x^z),y,z)
#define Tr2(x) (rotl(x, 30U) ^ rotl(x, 19U) ^ rotl(x, 10U))
#define Tr1(x) (rotl(x, 26U) ^ rotl(x, 21U) ^ rotl(x, 7U))
#define Wr2(x) (rotl(x, 25U) ^ rotl(x, 14U) ^ (x>>3U))
#define Wr1(x) (rotl(x, 15U) ^ rotl(x, 13U) ^ (x>>10U))

// Convert integer to N-digit decimal ASCII string (zero-padded)
void int_to_ascii(ulong n, uchar str[], int num_digits) {
    for (int i = num_digits - 1; i >= 0; i--) {
        str[i] = '0' + (n % 10);
        n /= 10;
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

// SHA256 implementation adapted from bfgminer_poclbm
// This processes a single 512-bit block using the optimized bfgminer_poclbm approach
void sha256_block_bfgminer_poclbm(uchar block[64], uint h[8]) {
    // Convert block to uint32 words (big-endian)
    uint w[64];
    for (int i = 0; i < 16; i++) {
        w[i] = ((uint)block[i * 4] << 24) |
               ((uint)block[i * 4 + 1] << 16) |
               ((uint)block[i * 4 + 2] << 8) |
               ((uint)block[i * 4 + 3]);
    }
    
    // Extend the 16 words into 64 words
    for (int i = 16; i < 64; i++) {
        w[i] = Wr1(w[i-2]) + w[i-7] + Wr2(w[i-15]) + w[i-16];
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
    
    // Main loop using bfgminer_poclbm-style operations
    for (int i = 0; i < 64; i++) {
        uint S1 = Tr1(e);
        uint ch = Ch(e, f, g);
        uint temp1 = h_val + S1 + ch + K[i] + w[i];
        uint S0 = Tr2(a);
        uint maj = Maj(a, b, c);
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

// Calculate SHA256 of input (generic implementation using bfgminer_poclbm SHA256)
void sha256_generic(uchar* input, int input_length, uchar output[32]) {
    // SHA256 initial hash values
    uint h[8] = {
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
    };
    
    // Process input in 512-bit (64-byte) blocks
    int num_blocks = (input_length + 9) / 64 + 1; // +9 for padding, +1 for length block
    int total_length = num_blocks * 64;
    
    uchar padded[2048]; // Max 2KB
    if (total_length > 2048) {
        // Too large, return zero hash
        for (int i = 0; i < 32; i++) {
            output[i] = 0;
        }
        return;
    }
    
    // Copy input
    for (int i = 0; i < input_length; i++) {
        padded[i] = input[i];
    }
    
    // Add padding: 0x80 followed by zeros
    padded[input_length] = 0x80;
    for (int i = input_length + 1; i < total_length - 8; i++) {
        padded[i] = 0;
    }
    
    // Add length in bits (big-endian, 64-bit)
    ulong bit_length = (ulong)input_length * 8;
    for (int i = 0; i < 8; i++) {
        padded[total_length - 8 + i] = (uchar)((bit_length >> (56 - i * 8)) & 0xff);
    }
    
    // Process each block
    for (int block = 0; block < num_blocks; block++) {
        uchar block_data[64];
        for (int i = 0; i < 64; i++) {
            block_data[i] = padded[block * 64 + i];
        }
        sha256_block_bfgminer_poclbm(block_data, h);
    }
    
    // Write output (32 bytes = 256 bits)
    for (int i = 0; i < 8; i++) {
        output[i * 4] = (uchar)((h[i] >> 24) & 0xff);
        output[i * 4 + 1] = (uchar)((h[i] >> 16) & 0xff);
        output[i * 4 + 2] = (uchar)((h[i] >> 8) & 0xff);
        output[i * 4 + 3] = (uchar)(h[i] & 0xff);
    }
}

__kernel void mine_nonce(
    __global uchar* base_serialized,
    int serialized_length,
    int nonce_offset,
    int difficulty,
    int base_nonce_low,
    int base_nonce_high,
    __global int* results,
    int num_digits
) {
    int global_id = get_global_id(0);
    
    // Reconstruct 64-bit base_nonce
    ulong base_nonce = ((ulong)(uint)base_nonce_high << 32) | ((ulong)(uint)base_nonce_low);
    ulong nonce = base_nonce + (ulong)global_id;
    
    // Calculate maximum nonce value
    ulong max_nonce = 0;
    if (num_digits <= 19) {
        max_nonce = 1;
        for (int i = 0; i < num_digits; i++) {
            max_nonce *= 10;
        }
        max_nonce -= 1;
    } else {
        max_nonce = 0xFFFFFFFFFFFFFFFFUL;
    }
    
    if (nonce > max_nonce) {
        results[global_id] = -1;
        return;
    }
    
    // Copy serialized string
    uchar serialized_copy[2048];
    if (serialized_length > 2048) {
        results[global_id] = -1;
        return;
    }
    
    for (int i = 0; i < serialized_length; i++) {
        serialized_copy[i] = base_serialized[i];
    }
    
    // Convert nonce to N-digit ASCII string
    uchar nonce_str[22];
    if (num_digits > 22) {
        results[global_id] = -1;
        return;
    }
    int_to_ascii(nonce, nonce_str, num_digits);
    
    // Replace nonce in the serialized string
    for (int i = 0; i < num_digits; i++) {
        serialized_copy[nonce_offset + i] = nonce_str[i];
    }
    
    // Calculate SHA256 using bfgminer_poclbm implementation
    uchar hash[32];
    sha256_generic(serialized_copy, serialized_length, hash);
    
    // Check if difficulty requirement is met
    int leading_zeros = count_leading_zero_bits(hash);
    
    if (leading_zeros >= difficulty) {
        results[global_id] = global_id;
    } else {
        results[global_id] = -1;
    }
}
