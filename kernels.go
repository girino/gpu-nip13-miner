// Copyright (c) 2025
// Licensed under Girino's Anarchist License (GAL)
// See LICENSE file or https://license.girino.org for details

package main

import _ "embed"

//go:embed kernel/mine.cl
var mineKernelSource string

//go:embed kernel/ckolivas-adapted.cl
var ckolivasKernelSource string
