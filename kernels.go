package main

import _ "embed"

//go:embed kernel/mine.cl
var mineKernelSource string

//go:embed kernel/ckolivas.cl
var ckolivasKernelSource string

//go:embed kernel/bfgminer-phatk-adapted.cl
var bfgminerPhatkKernelSource string

//go:embed kernel/bfgminer-diakgcn-adapted.cl
var bfgminerDiakgcnKernelSource string

//go:embed kernel/bfgminer-diablo-adapted.cl
var bfgminerDiabloKernelSource string

//go:embed kernel/bfgminer-poclbm-adapted.cl
var bfgminerPoclbmKernelSource string
