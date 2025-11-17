package main

import _ "embed"

//go:embed mine.cl
var mineKernelSource string

//go:embed ckolivas.cl
var ckolivasKernelSource string

//go:embed bfgminer-phatk-adapted.cl
var bfgminerPhatkKernelSource string

//go:embed bfgminer-diakgcn-adapted.cl
var bfgminerDiakgcnKernelSource string

//go:embed bfgminer-diablo-adapted.cl
var bfgminerDiabloKernelSource string

//go:embed bfgminer-poclbm-adapted.cl
var bfgminerPoclbmKernelSource string
