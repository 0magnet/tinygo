package builder

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

func phaseMem(name string) {
	if os.Getenv("TINYGO_PHASE_MEM") == "" {
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rss := ""
	if b, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "VmRSS") || strings.HasPrefix(l, "VmHWM") {
				rss += strings.Join(strings.Fields(l), " ") + " "
			}
		}
	}
	fmt.Fprintf(os.Stderr, "PHASEMEM %s goheap=%dMB gosys=%dMB %s\n", name, ms.HeapInuse>>20, ms.Sys>>20, rss)
}
