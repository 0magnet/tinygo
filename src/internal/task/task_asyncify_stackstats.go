//go:build scheduler.asyncify

package task

import "unsafe"

func (s *state) notePeak() {
	if s.canaryPtr == nil {
		return
	}
	save := uintptr(s.asyncifysp) - uintptr(unsafe.Pointer(s.canaryPtr))
	c := uintptr(s.top) - uintptr(s.csp)
	if save > s.peakSave {
		s.peakSave = save
	}
	if c > s.peakC {
		s.peakC = c
	}
}

// StackStats summarizes the peak stack use of live goroutines: the largest C
// stack and save area, the sum of each task's peak, and how many tasks peaked
// above 16, 32, 64 and 128 KiB.
func StackStats() (maxC, maxSave, sumPeak uint64, over [4]uint64) {
	for t := stackTasks; t != nil; t = t.state.nextStack {
		c, save := uint64(highWaterC(&t.state)), uint64(t.state.peakSave)
		if c > maxC {
			maxC = c
		}
		if save > maxSave {
			maxSave = save
		}
		p := c + save
		sumPeak += p
		for i, lim := range [4]uint64{16 << 10, 32 << 10, 64 << 10, 128 << 10} {
			if p > lim {
				over[i]++
			}
		}
	}
	return
}

// highWaterC finds the deepest C stack use from the first word ever written
// above the save area, since stacks start zeroed.
func highWaterC(s *state) uintptr {
	if s.canaryPtr == nil {
		return 0
	}
	base := uintptr(unsafe.Pointer(s.canaryPtr))
	top := uintptr(s.top)
	p := base + s.peakSave
	p = (p + unsafe.Sizeof(p) - 1) &^ (unsafe.Sizeof(p) - 1)
	for p < top && *(*uintptr)(unsafe.Pointer(p)) == 0 {
		p += unsafe.Sizeof(p)
	}
	if hw := top - p; hw > s.peakC {
		return hw
	}
	return s.peakC
}
