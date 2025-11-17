package main

import _ "embed"

//go:embed kernel/mine.cl
var mineKernelSource string

//go:embed kernel/ckolivas-adapted.cl
var ckolivasKernelSource string
