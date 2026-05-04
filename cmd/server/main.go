package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"go-webttyd/internal/config"
	"go-webttyd/internal/server"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	if cfg.AutokillPort {
		killProcessOnPort(cfg.Port)
	}

	srv := server.New(cfg)
	log.Printf("listening on http://localhost:%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
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

	log.Printf("killing existing process %d on port %s", pid, port)

	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Kill()
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
