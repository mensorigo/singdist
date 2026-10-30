package agent

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// cpuSampler measures CPU usage over the interval between
// successive Status() calls (the panel probes every 30s), not over
// a fixed 100ms window. /proc/stat ticks are 10ms each, so a 100ms
// window quantizes any usage under one tick to exactly 0.00% —
// that is why an idle-but-alive box always showed 0.00%. Over
// 30s the same tick is 1/3000 of the window, so even 0.1% usage
// shows up. A first call with no prior sample takes a 1s sample
// instead of returning 0.
type cpuSampler struct {
	mu        sync.Mutex
	lastTotal uint64
	lastIdle  uint64
	lastPct   float64
	lastTime  time.Time
	hasLast   bool
}

var sampler cpuSampler

func (s *cpuSampler) sample() (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total, idle, ok := readCPUStat()
	if !ok {
		return 0, false
	}
	if !s.hasLast {
		// First call: 1s window, not 100ms, so the first value
		// is already meaningful instead of a quantized 0.
		time.Sleep(1 * time.Second)
		total2, idle2, ok2 := readCPUStat()
		if !ok2 || total2 <= total {
			return 0, false
		}
		pct := float64(total2-total-(idle2-idle)) / float64(total2-total) * 100
		s.lastTotal, s.lastIdle, s.lastPct, s.lastTime, s.hasLast = total2, idle2, pct, time.Now(), true
		return pct, true
	}
	// A call too soon after the last one would re-quantize to
	// 0.00%: return the cached value instead of updating.
	if time.Since(s.lastTime) < 2*time.Second {
		return s.lastPct, true
	}
	if total <= s.lastTotal {
		return s.lastPct, true
	}
	pct := float64(total-s.lastTotal-(idle-s.lastIdle)) / float64(total-s.lastTotal) * 100
	s.lastTotal, s.lastIdle, s.lastPct, s.lastTime = total, idle, pct, time.Now()
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

// SystemStats is a snapshot of the node's own load and memory,
// read from /proc (Linux only; empty on other platforms). CpuPct
// is the CPU usage percentage over the interval since the last
// Status() call; Load is the 1/5/15-minute load average, kept
// for context.
type SystemStats struct {
	CpuPct    float64 `json:"cpuPct"`
	Load1     float64 `json:"load1"`
	Load5     float64 `json:"load5"`
	Load15    float64 `json:"load15"`
	MemTotal  int64   `json:"memTotal"`
	MemUsed   int64   `json:"memUsed"`
	MemPct    float64 `json:"memPct"`
	DiskTotal int64   `json:"diskTotal"`
	DiskUsed  int64   `json:"diskUsed"`
	DiskPct   float64 `json:"diskPct"`
	Available bool    `json:"available"`
}

func readCPUStat() (total, idle uint64, ok bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 5 {
			return 0, 0, false
		}
		for i, s := range f[1:] {
			v, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			total += v
			// idle = idle + iowait (fields 4 and 5, 0-indexed 3 and 4).
			if i == 3 || i == 4 {
				idle += v
			}
		}
		return total, idle, true
	}
	return 0, 0, false
}

func readSystemStats() SystemStats {
	var st SystemStats
	if pct, ok := sampler.sample(); ok {
		st.CpuPct = pct
		st.Available = true
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		if len(f) >= 3 {
			st.Load1, _ = strconv.ParseFloat(f[0], 64)
			st.Load5, _ = strconv.ParseFloat(f[1], 64)
			st.Load15, _ = strconv.ParseFloat(f[2], 64)
			st.Available = true
		}
	}
	// Disk usage of the root filesystem, where the agent, the
	// core and its data live. Statfs is stdlib, so the agent
	// keeps its zero-dependency rule.
	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err == nil && fs.Blocks > 0 {
		st.DiskTotal = int64(fs.Blocks) * int64(fs.Bsize)
		st.DiskUsed = int64(fs.Blocks-fs.Bfree) * int64(fs.Bsize)
		if st.DiskTotal > 0 {
			st.DiskPct = float64(st.DiskUsed) / float64(st.DiskTotal) * 100
			st.Available = true
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, avail int64
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			v, err := strconv.ParseInt(f[1], 10, 64)
			if err != nil {
				continue
			}
			switch f[0] {
			case "MemTotal:":
				total = v * 1024
			case "MemAvailable:":
				avail = v * 1024
			}
		}
		if total > 0 {
			st.MemTotal = total
			st.MemUsed = total - avail
			st.MemPct = float64(st.MemUsed) / float64(total) * 100
			st.Available = true
		}
	}
	return st
}
