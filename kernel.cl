__kernel void count_chars(__global char* input, __global int* output, int length) {
    int count = 0;
    for (int i = 0; i < length; i++) {
        if (input[i] != 0) {
            count++;
        }
    }
    output[0] = count;
}

