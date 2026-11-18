package hoststat

import "runtime"

func numCPU() int { return runtime.NumCPU() }
