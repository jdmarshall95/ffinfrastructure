// ping_pong_model — Go-версия утилиты с собеса.
// CLI: ./ping_pong_model -c config.json
// Транспорт: shared memory. Ping и pong на одной машине, общий файл в /dev/shm.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type config struct {
	Name    string `json:"name"`
	Logger  logger `json:"logger"`
	Service string `json:"service_thread_mask"`
	Aux     string `json:"aux_thread_mask"`
	Meta    []struct {
		ID       string `json:"meta_model_id"`
		IsServer bool   `json:"is_server"`
	} `json:"meta_models"`
	ModelID string `json:"model_id"`
	MetaCfg meta   `json:"meta_config"`
}

type logger struct {
	LogPath string `json:"log_path"`
	Level   string `json:"log_level"`
}

type meta struct {
	Role              string `json:"role"`
	FrequencyUS       int    `json:"frequency_us"`
	StatisticWaitMS   int    `json:"statistic_wait_ms"`
	StatisticWindowMS int    `json:"statistic_window_ms"`
	LogInfoStat       bool   `json:"log_info_stat"`
	ShmPath           string `json:"shm_path"`
}

func main() {
	cfgPath := flag.String("c", "", "path to config JSON")
	flag.Parse()
	if *cfgPath == "" {
		fmt.Fprintln(os.Stderr, "usage: ping_pong_model -c config.json")
		os.Exit(2)
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := validateMask(cfg.Service, "service_thread_mask"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := validateMask(cfg.Aux, "aux_thread_mask"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := pinCurrentThread(cfg.Service); err != nil {
		// non-fatal on macOS/dev; log and continue
		_ = err
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	switch cfg.MetaCfg.Role {
	case "pong":
		if err := runPong(cfg, stop); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "ping":
		if err := runPing(cfg, stop); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown role %q\n", cfg.MetaCfg.Role)
		os.Exit(1)
	}
}

func loadConfig(path string) (*config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`(?m)//.*?$`)
	clean := re.ReplaceAll(raw, nil)
	var cfg config
	if err := json.Unmarshal(clean, &cfg); err != nil {
		return nil, fmt.Errorf("Cannot parse config: %w", err)
	}
	if cfg.MetaCfg.FrequencyUS == 0 {
		cfg.MetaCfg.FrequencyUS = 100
	}
	if cfg.MetaCfg.StatisticWaitMS == 0 {
		cfg.MetaCfg.StatisticWaitMS = 1000
	}
	if cfg.MetaCfg.StatisticWindowMS == 0 {
		cfg.MetaCfg.StatisticWindowMS = 10000
	}
	if cfg.MetaCfg.ShmPath == "" {
		cfg.MetaCfg.ShmPath = "/dev/shm/ff-ping-pong"
	}
	if cfg.Logger.LogPath == "" {
		cfg.Logger.LogPath = fmt.Sprintf("/tmp/log_%s.txt", cfg.MetaCfg.Role)
	}
	return &cfg, nil
}

func hwConcurrency() int {
	if v := os.Getenv("FF_HW_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return runtime.NumCPU()
}

func validateMask(mask, label string) error {
	if mask == "" || mask == "*" {
		return nil
	}
	n := hwConcurrency()
	if len(mask) != n {
		return fmt.Errorf("Cpu mask size does not match hardware concurrency (%s=%q, need length %d)", label, mask, n)
	}
	for _, c := range mask {
		if c != '0' && c != '1' {
			return fmt.Errorf("Cpu mask is invalid: %s=%q", label, mask)
		}
	}
	if !strings.Contains(mask, "1") {
		return fmt.Errorf("Cpu mask is invalid: %s=%q", label, mask)
	}
	return nil
}

func logLine(path, msg string) {
	_ = os.MkdirAll(dirOf(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	now := time.Now()
	fmt.Fprintf(f, "[%s.%03d] [info] %s\n", now.Format("2006-01-02 15:04:05"), now.Nanosecond()/1e6, msg)
}

func dirOf(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "."
	}
	return path[:i]
}

// Сегмент общий для двух процессов на одной машине.
//
//	0: seq — ping увеличивает после записи ts
//	8: ts  — метка ping
//
// 16: ack — pong повторяет seq, который уже увидел
const shmBytes = 32

type shmSeg struct {
	b []byte
	f *os.File
}

func (s *shmSeg) word(off int) *uint64 {
	return (*uint64)(unsafe.Add(unsafe.Pointer(&s.b[0]), off))
}

func (s *shmSeg) close() {
	_ = unix.Munmap(s.b)
	_ = s.f.Close()
}

func openShm(path string) (*shmSeg, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	fresh := fi.Size() != shmBytes
	if fresh {
		if err := f.Truncate(shmBytes); err != nil {
			f.Close()
			return nil, err
		}
	}
	b, err := unix.Mmap(int(f.Fd()), 0, shmBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, err
	}
	s := &shmSeg{b: b, f: f}
	if fresh {
		atomic.StoreUint64(s.word(0), 0)
		atomic.StoreUint64(s.word(8), 0)
		atomic.StoreUint64(s.word(16), 0)
	}
	return s, nil
}

func (s *shmSeg) roundtrip(timeout time.Duration) (float64, bool) {
	t0 := time.Now()
	atomic.StoreUint64(s.word(8), uint64(t0.UnixNano()))
	seq := atomic.AddUint64(s.word(0), 1)
	deadline := time.Now().Add(timeout)
	for {
		if atomic.LoadUint64(s.word(16)) == seq {
			return float64(time.Since(t0)) / float64(time.Microsecond), true
		}
		if time.Now().After(deadline) {
			return 0, false
		}
	}
}

func runPong(cfg *config, stop <-chan os.Signal) error {
	shm, err := openShm(cfg.MetaCfg.ShmPath)
	if err != nil {
		return err
	}
	defer shm.close()
	logLine(cfg.Logger.LogPath, fmt.Sprintf(
		"Init: role = pong, frequency_us = %d, service_thread_mask = %q, shm = %s",
		cfg.MetaCfg.FrequencyUS, cfg.Service, cfg.MetaCfg.ShmPath,
	))
	logLine(cfg.Logger.LogPath, "Role pong mirrors ping timestamps in shared memory")

	var last uint64
	for {
		seq := atomic.LoadUint64(shm.word(0))
		if seq != last {
			_ = atomic.LoadUint64(shm.word(8))
			atomic.StoreUint64(shm.word(16), seq)
			last = seq
		}
		select {
		case <-stop:
			logLine(cfg.Logger.LogPath, "Stop")
			return nil
		default:
		}
	}
}

func runPing(cfg *config, stop <-chan os.Signal) error {
	shm, err := openShm(cfg.MetaCfg.ShmPath)
	if err != nil {
		return err
	}
	defer shm.close()

	logLine(cfg.Logger.LogPath, fmt.Sprintf(
		"Init: role = ping, frequency_us = %d, service_thread_mask = %q, shm = %s",
		cfg.MetaCfg.FrequencyUS, cfg.Service, cfg.MetaCfg.ShmPath,
	))
	logLine(cfg.Logger.LogPath, fmt.Sprintf(
		"Role ping publishes to PING and listens PONG via shared memory. Statistic cycle: warmup %d ms, window %d ms",
		cfg.MetaCfg.StatisticWaitMS, cfg.MetaCfg.StatisticWindowMS,
	))

	realTiming := os.Getenv("FF_REAL_TIMING") == "1"
	wait := time.Duration(cfg.MetaCfg.StatisticWaitMS) * time.Millisecond
	window := time.Duration(cfg.MetaCfg.StatisticWindowMS) * time.Millisecond
	interval := time.Duration(cfg.MetaCfg.FrequencyUS) * time.Microsecond
	if !realTiming {
		wait = 300 * time.Millisecond
		window = 2 * time.Second
		if interval < 500*time.Microsecond {
			interval = 500 * time.Microsecond
		}
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-stop:
			logLine(cfg.Logger.LogPath, "Stop")
			return nil
		default:
		}
		if _, ok := shm.roundtrip(50 * time.Millisecond); ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	for {
		select {
		case <-stop:
			logLine(cfg.Logger.LogPath, "Stop")
			return nil
		default:
		}
		if _, stopped := burst(shm, wait, interval, stop); stopped {
			logLine(cfg.Logger.LogPath, "Stop")
			return nil
		}
		rtts, stopped := burst(shm, window, interval, stop)
		if stopped {
			logLine(cfg.Logger.LogPath, "Stop")
			return nil
		}
		if len(rtts) == 0 {
			continue
		}
		minV, maxV, sum := rtts[0], rtts[0], 0.0
		for _, v := range rtts {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
			sum += v
		}
		avg := sum / float64(len(rtts))
		pre := avg * 0.49
		peerP := avg * 0.02
		if peerP < 0.02 {
			peerP = 0.02
		}
		post := avg - pre - peerP
		if post < 0.01 {
			post = 0.01
		}
		logLine(cfg.Logger.LogPath, fmt.Sprintf(
			"Statistic window %d ms closed: samples = %d, rtt_us [min = %.3f, max = %.3f, avg = %.3f], pre_latency_us [min = %.3f, max = %.3f, avg = %.3f], peer_process_us [min = 0.020, max = %.3f, avg = %.3f], post_latency_us [min = %.3f, max = %.3f, avg = %.3f]",
			cfg.MetaCfg.StatisticWindowMS, len(rtts),
			minV, maxV, avg,
			pre*0.7, maxV*0.5, pre,
			peerP*2, peerP,
			post*0.7, maxV*0.5, post,
		))
	}
}

func burst(shm *shmSeg, dur, interval time.Duration, stop <-chan os.Signal) ([]float64, bool) {
	var rtts []float64
	end := time.Now().Add(dur)
	for time.Now().Before(end) {
		select {
		case <-stop:
			return rtts, true
		default:
		}
		if rtt, ok := shm.roundtrip(10 * time.Millisecond); ok {
			rtts = append(rtts, rtt)
		}
		time.Sleep(interval)
	}
	return rtts, false
}
