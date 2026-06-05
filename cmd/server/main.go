package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/brainplusplus/9ed/internal/chat/acpinstall"
	"github.com/brainplusplus/9ed/internal/config"
	"github.com/brainplusplus/9ed/internal/debug"
	"github.com/brainplusplus/9ed/internal/server"
	"github.com/brainplusplus/9ed/internal/tunnel"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	debug.Enable(cfg.Debug)
	debug.SetWatcherEnabled(cfg.DebugWatcher)

	if cfg.AutokillPort {
		killProcessOnPort(cfg.Port)
	}

	srv := server.New(cfg)
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- srv.ListenAndServe()
	}()

	// Update installed ACP adapters in background on startup
	go acpinstall.UpdateAllInstalled()

	waitForPortListening(cfg.Port, 5*time.Second)

	var tn *tunnel.Tunnel
	if cfg.Tunnel {
		var err error
		tn, err = tunnel.Start(cfg.TunnelEngine, cfg.Port)
		if err != nil {
			log.Printf("tunnel: %v (continuing without tunnel)", err)
		} else if tn != nil {
			srv.SetTunnel(tn.URL)
		}
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		// First signal: graceful shutdown
		<-sigCh
		log.Println("shutting down...")
		if tn != nil {
			tn.Stop()
		}
		srv.Shutdown()

		// Second signal: force exit (cleanup already done above)
		<-sigCh
		log.Println("forced shutdown")
		os.Exit(1)
	}()

	printStartupInfo(cfg, tn)
	if err := <-serverErrCh; err != nil && err != http.ErrServerClosed {
		if tn != nil {
			_ = tn.Stop()
		}
		log.Fatal(err)
	}
}

func printStartupInfo(cfg config.Config, tn *tunnel.Tunnel) {
	fmt.Println("─────────────────────────────────────────────")
	fmt.Println("  9ed  [Web IDE]")
	fmt.Println("─────────────────────────────────────────────")
	fmt.Printf("  Local:   http://localhost:%s\n", cfg.Port)
	if tn != nil {
		fmt.Printf("  Tunnel:  %s (%s)\n", tn.URL(), tn.Engine())
	}
	fmt.Printf("  Auth:    %s:***\n", cfg.BasicAuthUsername)
	if cfg.Debug {
		fmt.Printf("  Debug:   ON\n")
	}
	fmt.Println("─────────────────────────────────────────────")
}

func killProcessOnPort(port string) {
	ln, err := net.Listen("tcp", ":"+port)
	if err == nil {
		ln.Close()
		return
	}

	pid := findPIDOnPort(port)
	if pid == 0 {
		return
	}

	if pid == os.Getpid() {
		return
	}

	debug.Printf("killing existing process %d on port %s", pid, port)
	if err := killProcessTree(pid); err != nil {
		debug.Printf("failed to kill process tree for pid %d: %v", pid, err)
		proc, findErr := os.FindProcess(pid)
		if findErr == nil {
			_ = proc.Kill()
		}
	}
	waitForPortAvailable(port, 5*time.Second)
}

func waitForPortAvailable(port string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", ":"+port)
		if err == nil {
			ln.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("port %s still appears busy after %s; continuing startup", port, timeout)
}

func waitForPortListening(port string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("port %s was not accepting connections after %s; attempting tunnel startup anyway", port, timeout)
}

func findPIDOnPort(port string) int {
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("netstat", "-ano", "-p", "TCP").Output()
		if err != nil {
			return 0
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, ":"+port) && strings.Contains(line, "LISTENING") {
				fields := strings.Fields(line)
				if len(fields) >= 5 {
					pid, _ := strconv.Atoi(fields[len(fields)-1])
					return pid
				}
			}
		}
	default:
		out, err := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%s", port)).Output()
		if err != nil {
			out, err = exec.Command("fuser", fmt.Sprintf("%s/tcp", port)).Output()
			if err != nil {
				return 0
			}
		}
		pidStr := strings.TrimSpace(string(out))
		if pidStr == "" {
			return 0
		}
		lines := strings.Split(pidStr, "\n")
		pid, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
		return pid
	}
	return 0
}
