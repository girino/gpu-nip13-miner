// SHA256 OpenCL Kernel
// Based on the SHA-256 algorithm specification

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

__kernel void sha256(__global uchar* input, __global uchar* output, int input_length) {
    // SHA256 initial hash values
    uint h[8] = {
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
    };
    
    // Calculate padding
    // SHA256 processes messages in 512-bit (64-byte) blocks
    int original_bit_length = input_length * 8;
    int padding_length = (64 - ((input_length + 9) % 64)) % 64;
    int total_length = input_length + 1 + padding_length + 8; // +1 for 0x80, +8 for length
    
    // For simplicity, we'll process one block (up to 55 bytes of input)
    // This kernel handles inputs up to 55 bytes (448 bits, leaving 64 bits for length)
    if (input_length > 55) {
        // For inputs longer than 55 bytes, we'd need multiple blocks
        // This is a simplified version for demonstration
        return;
    }
    
    // Prepare the message block (64 bytes)
    uchar block[64];
    
    // Copy input
    for (int i = 0; i < input_length; i++) {
        block[i] = input[i];
    }
    
    // Add padding: 0x80 followed by zeros
    block[input_length] = 0x80;
    for (int i = input_length + 1; i < 56; i++) {
        block[i] = 0;
    }
    
    // Add length in bits (big-endian, 64 bits)
    // Store length in the last 8 bytes
    ulong bit_length = (ulong)original_bit_length;
    for (int i = 0; i < 8; i++) {
        block[56 + i] = (uchar)((bit_length >> (56 - i * 8)) & 0xff);
    }
    
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
    
    // Write output (32 bytes = 256 bits)
    for (int i = 0; i < 8; i++) {
        output[i * 4] = (uchar)((h[i] >> 24) & 0xff);
        output[i * 4 + 1] = (uchar)((h[i] >> 16) & 0xff);
        output[i * 4 + 2] = (uchar)((h[i] >> 8) & 0xff);
        output[i * 4 + 3] = (uchar)(h[i] & 0xff);
    }
}

