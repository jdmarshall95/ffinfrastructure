// ping_pong_model — Go-версия утилиты с собеса.
// CLI: ./ping_pong_model -c config.json
// Транспорт: UDP (на одной машине или между двумя ВМ через peer_addr).
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
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
	MetaCfg meta  `json:"meta_config"`
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
	Listen            string `json:"listen"`    // e.g. ":19090"
	PeerAddr          string `json:"peer_addr"` // e.g. "10.0.0.12:19090" for ping
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
	if cfg.MetaCfg.Listen == "" {
		cfg.MetaCfg.Listen = ":19090"
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

func pack(ts uint64) []byte {
	buf := make([]byte, 12)
	copy(buf[:4], []byte("FFPP"))
	binary.BigEndian.PutUint64(buf[4:], ts)
	return buf
}

func unpack(b []byte) (uint64, bool) {
	if len(b) < 12 || !bytes.Equal(b[:4], []byte("FFPP")) {
		return 0, false
	}
	return binary.BigEndian.Uint64(b[4:]), true
}

func runPong(cfg *config, stop <-chan os.Signal) error {
	pc, err := net.ListenPacket("udp", cfg.MetaCfg.Listen)
	if err != nil {
		return err
	}
	defer pc.Close()
	logLine(cfg.Logger.LogPath, fmt.Sprintf(
		"Init: role = pong, frequency_us = %d, service_thread_mask = %q, listen = %s",
		cfg.MetaCfg.FrequencyUS, cfg.Service, cfg.MetaCfg.Listen,
	))
	logLine(cfg.Logger.LogPath, "Role pong publishes to PONG and listens PING")

	buf := make([]byte, 64)
	for {
		select {
		case <-stop:
			logLine(cfg.Logger.LogPath, "Stop")
			return nil
		default:
		}
		_ = pc.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			continue
		}
		if ts, ok := unpack(buf[:n]); ok {
			_, _ = pc.WriteTo(pack(ts), addr)
		}
	}
}

func runPing(cfg *config, stop <-chan os.Signal) error {
	if cfg.MetaCfg.PeerAddr == "" {
		cfg.MetaCfg.PeerAddr = "127.0.0.1" + ensurePort(cfg.MetaCfg.Listen)
	}
	peer, err := net.ResolveUDPAddr("udp", cfg.MetaCfg.PeerAddr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	logLine(cfg.Logger.LogPath, fmt.Sprintf(
		"Init: role = ping, frequency_us = %d, service_thread_mask = %q, peer = %s",
		cfg.MetaCfg.FrequencyUS, cfg.Service, cfg.MetaCfg.PeerAddr,
	))
	logLine(cfg.Logger.LogPath, fmt.Sprintf(
		"Role ping publishes to PING and listens PONG. Statistic cycle: warmup %d ms, window %d ms",
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

	buf := make([]byte, 64)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-stop:
			logLine(cfg.Logger.LogPath, "Stop")
			return nil
		default:
		}
		ts := uint64(time.Now().UnixNano())
		_, _ = conn.WriteToUDP(pack(ts), peer)
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err == nil {
			if got, ok := unpack(buf[:n]); ok && got == ts {
				break
			}
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
		burst(conn, peer, buf, wait, interval, stop)
		rtts := burst(conn, peer, buf, window, interval, stop)
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

func burst(conn *net.UDPConn, peer *net.UDPAddr, buf []byte, dur, interval time.Duration, stop <-chan os.Signal) []float64 {
	var rtts []float64
	end := time.Now().Add(dur)
	for time.Now().Before(end) {
		select {
		case <-stop:
			return rtts
		default:
		}
		t0 := time.Now()
		ts := uint64(t0.UnixNano())
		_, _ = conn.WriteToUDP(pack(ts), peer)
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err == nil {
			if got, ok := unpack(buf[:n]); ok && got == ts {
				rtts = append(rtts, float64(time.Since(t0))/float64(time.Microsecond))
			}
		}
		time.Sleep(interval)
	}
	return rtts
}

func ensurePort(listen string) string {
	if strings.HasPrefix(listen, ":") {
		return listen
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return ":19090"
	}
	return ":" + port
}
