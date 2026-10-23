package dash

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// cpuReader computes host CPU utilisation from /proc/stat deltas between
// successive Percent() calls.
type cpuReader struct {
	mu      sync.Mutex
	prevTot uint64
	prevIdl uint64
	primed  bool
	pct     float64
}

func newCPUReader() *cpuReader { return &cpuReader{} }

func readCPUTimes() (total, idle uint64, ok bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var vals []uint64
		for _, f := range fields {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			vals = append(vals, v)
		}
		if len(vals) < 4 {
			return 0, 0, false
		}
		for _, v := range vals {
			total += v
		}
		idle = vals[3] // idle
		if len(vals) > 4 {
			idle += vals[4] // iowait counts as idle for utilisation purposes
		}
		return total, idle, true
	}
	return 0, 0, false
}

// Percent returns the CPU utilisation since the previous call (0 on the
// first call or when /proc/stat is unavailable).
func (r *cpuReader) Percent() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	total, idle, ok := readCPUTimes()
	if !ok {
		return 0
	}
	if r.primed && total > r.prevTot {
		dt := float64(total - r.prevTot)
		di := float64(idle - r.prevIdl)
		if dt > 0 {
			r.pct = 100 * (1 - di/dt)
			if r.pct < 0 {
				r.pct = 0
			}
			if r.pct > 100 {
				r.pct = 100
			}
		}
	}
	r.prevTot, r.prevIdl, r.primed = total, idle, true
	return r.pct
}

func readMeminfo() (totalKB, availKB int64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			totalKB = parseMeminfoValue(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			availKB = parseMeminfoValue(line)
		}
	}
	return totalKB, availKB, totalKB > 0
}

func parseMeminfoValue(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// --- OS / load / disk / core uptime ----------------------------------------

var (
	osNameOnce sync.Once
	osNameVal  string
)

// osPrettyName returns the OS PRETTY_NAME from /etc/os-release (cached),
// or an empty string when unavailable.
func osPrettyName() string {
	osNameOnce.Do(func() {
		f, err := os.Open("/etc/os-release")
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				osNameVal = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
				return
			}
		}
	})
	return osNameVal
}

// loadAvg returns the 1/5/15-minute load averages from /proc/loadavg.
func loadAvg() [3]float64 {
	var out [3]float64
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return out
	}
	fields := strings.Fields(string(b))
	for i := 0; i < 3 && i < len(fields); i++ {
		if v, err := strconv.ParseFloat(fields[i], 64); err == nil {
			out[i] = v
		}
	}
	return out
}

// diskUsage returns used and total bytes for the filesystem at path.
func diskUsage(path string) (used, total int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	total = int64(st.Blocks) * int64(st.Bsize)
	avail := int64(st.Bavail) * int64(st.Bsize)
	if total > 0 {
		used = total - avail
		if used < 0 {
			used = 0
		}
	}
	return used, total
}

// coreUptimeSecs estimates how long the sing-box process has been running
// by finding it in /proc and comparing its start time against the system
// uptime. Returns 0 when no sing-box process is visible.
func coreUptimeSecs() int64 {
	uptime := systemUptimeSecs()
	if uptime <= 0 {
		return 0
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	const clockHz = 100.0 // USER_HZ on Linux x86_64/arm64
	var best int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil || strings.TrimSpace(string(comm)) != "sing-box" {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// Fields after the parenthesised comm: field 22 overall is starttime.
		s := string(stat)
		idx := strings.LastIndex(s, ")")
		if idx < 0 {
			continue
		}
		rest := strings.Fields(s[idx+1:])
		// rest[0] is field 3 (state); starttime is field 22 -> rest[19].
		if len(rest) < 20 {
			continue
		}
		startTicks, err := strconv.ParseUint(rest[19], 10, 64)
		if err != nil {
			continue
		}
		age := uptime - int64(float64(startTicks)/clockHz)
		if age > best {
			best = age
		}
	}
	return best
}

// netTotals returns cumulative NIC byte counters (receive, transmit)
// summed over all interfaces except loopback, from /proc/net/dev. These
// are the host's network totals since boot, matching the old panel's
// 网络累计 card (which read NIC counters, not core counters).
func netTotals() (rxBytes, txBytes int64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colon])
		if iface == "" || iface == "lo" {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		// receive bytes is field 0, transmit bytes is field 8.
		if len(fields) < 9 {
			continue
		}
		if v, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			rxBytes += v
		}
		if v, err := strconv.ParseInt(fields[8], 10, 64); err == nil {
			txBytes += v
		}
	}
	return rxBytes, txBytes
}

// systemUptimeSecs returns seconds since boot from /proc/uptime.
func systemUptimeSecs() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

func memTotal() int64 {
	total, _, ok := readMeminfo()
	if !ok {
		return 0
	}
	return total * 1024
}

func memUsed() int64 {
	total, avail, ok := readMeminfo()
	if !ok || avail > total {
		return 0
	}
	return (total - avail) * 1024
}
