package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	frontendPort = 6191
	backendPort  = 6192
)

type serviceSpec struct {
	name    string
	port    int
	workDir string
	command string
	args    []string
	logPath string
}

type startedProcess struct {
	name string
	cmd  *exec.Cmd
}

type tailscaleStatus struct {
	BackendState string `json:"BackendState"`
	Self struct {
		DNSName      string   `json:"DNSName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
	} `json:"Self"`
}

func main() {
	if runtime.GOOS != "windows" {
		fatal("This launcher currently supports Windows only.")
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		fatal(err.Error())
	}
	if err := ensurePath(filepath.Join(repoRoot, "backend", "video-server.exe")); err != nil {
		fatal(err.Error())
	}
	if err := ensurePath(filepath.Join(repoRoot, "dist", "index.html")); err != nil {
		fatal(err.Error())
	}
	if err := ensurePath(filepath.Join(repoRoot, "node_modules", "vite", "bin", "vite.js")); err != nil {
		fatal(err.Error())
	}

	nodePath, err := exec.LookPath("node")
	if err != nil {
		fatal("node.exe was not found. Install Node.js first.")
	}

	tmpDir := filepath.Join(repoRoot, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		fatal(fmt.Sprintf("Failed to create log directory: %v", err))
	}

	specs := []serviceSpec{
		{
			name:    "backend",
			port:    backendPort,
			workDir: filepath.Join(repoRoot, "backend"),
			command: filepath.Join(repoRoot, "backend", "video-server.exe"),
			logPath: filepath.Join(tmpDir, "launcher-backend.log"),
		},
		{
			name:    "frontend",
			port:    frontendPort,
			workDir: repoRoot,
			command: nodePath,
			args: []string{
				filepath.Join(repoRoot, "node_modules", "vite", "bin", "vite.js"),
				"preview",
				"--host", "0.0.0.0",
				"--port", fmt.Sprintf("%d", frontendPort),
			},
			logPath: filepath.Join(tmpDir, "launcher-frontend.log"),
		},
	}

	fmt.Println("61 launcher")
	fmt.Println(strings.Repeat("=", 48))
	fmt.Printf("Repo: %s\n", repoRoot)
	fmt.Printf("Logs: %s\n", tmpDir)
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var started []startedProcess
	cleanup := func() {
		for i := len(started) - 1; i >= 0; i-- {
			proc := started[i]
			if proc.cmd != nil && proc.cmd.Process != nil {
				_ = proc.cmd.Process.Kill()
				_, _ = proc.cmd.Process.Wait()
			}
		}
	}
	defer cleanup()

	for _, spec := range specs {
		if isPortOpen(spec.port) {
			fmt.Printf("[%s] port %d is already in use, skipping start.\n", spec.name, spec.port)
			continue
		}
		cmd, err := startService(spec)
		if err != nil {
			fatal(fmt.Sprintf("[%s] start failed: %v\nLog file: %s", spec.name, err, spec.logPath))
		}
		started = append(started, startedProcess{name: spec.name, cmd: cmd})
		fmt.Printf("[%s] started, waiting for port %d...\n", spec.name, spec.port)
	}

	for _, spec := range specs {
		if err := waitForPort(ctx, spec.port, 45*time.Second); err != nil {
			fatal(fmt.Sprintf("[%s] port %d not ready: %v\nCheck log: %s", spec.name, spec.port, err, spec.logPath))
		}
		fmt.Printf("[%s] port %d is ready.\n", spec.name, spec.port)
	}

	fmt.Println()
	fmt.Println("Access URLs:")
	fmt.Printf("- Local home:  http://127.0.0.1:%d/\n", frontendPort)
	fmt.Printf("- Local admin: http://127.0.0.1:%d/admin\n", frontendPort)
	for _, ip := range localIPv4Addrs() {
		fmt.Printf("- LAN home:    http://%s:%d/\n", ip, frontendPort)
		fmt.Printf("- LAN admin:   http://%s:%d/admin\n", ip, frontendPort)
	}
	for _, line := range tailscaleAccessLines(frontendPort) {
		fmt.Println(line)
	}
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop the processes started by this launcher.")

	<-ctx.Done()
	fmt.Println()
	fmt.Println("Shutdown signal received, stopping services...")
}

func findRepoRoot() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exePath)
	for {
		if looksLikeRepoRoot(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("repo root not found; place the launcher inside the repository directory")
		}
		dir = parent
	}
}

func looksLikeRepoRoot(dir string) bool {
	return fileExists(filepath.Join(dir, "package.json")) &&
		fileExists(filepath.Join(dir, "backend", "video-server.exe")) &&
		fileExists(filepath.Join(dir, "dist", "index.html"))
}

func ensurePath(path string) error {
	if !fileExists(path) {
		return fmt.Errorf("missing required file: %s", path)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isPortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 700*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitForPort(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if isPortOpen(port) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for port")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func startService(spec serviceSpec) (*exec.Cmd, error) {
	logFile, err := os.OpenFile(spec.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(spec.command, spec.args...)
	cmd.Dir = spec.workDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	return cmd, nil
}

func localIPv4Addrs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP == nil {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}
			if !isLikelyLANIPv4(iface.Name, ip) {
				continue
			}
			value := ip.String()
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func isLikelyLANIPv4(ifaceName string, ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() {
		return false
	}
	text := strings.ToLower(ifaceName)
	for _, blocked := range []string{
		"vethernet", "wsl", "hyper-v", "virtualbox", "vmware", "zerotier", "docker",
	} {
		if strings.Contains(text, blocked) {
			return false
		}
	}
	if ip[0] == 169 && ip[1] == 254 {
		return false
	}
	return ip[0] == 10 ||
		(ip[0] == 192 && ip[1] == 168) ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31)
}

func tailscaleAccessLines(port int) []string {
	tailscalePath, err := exec.LookPath("tailscale")
	if err != nil {
		return nil
	}
	out, err := exec.Command(tailscalePath, "status", "--json").Output()
	if err != nil {
		return []string{
			"- Tailscale: not connected. Run scripts\\start-91-tailscale.ps1 after login.",
		}
	}

	var status tailscaleStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return []string{
			"- Tailscale: installed, but status could not be parsed.",
		}
	}
	if !strings.EqualFold(strings.TrimSpace(status.BackendState), "Running") {
		return []string{
			"- Tailscale: service installed, but tailnet is not connected. Run scripts\\start-91-tailscale.ps1 after turning Tailscale on.",
		}
	}

	dnsName := strings.TrimSuffix(strings.TrimSpace(status.Self.DNSName), ".")
	lines := []string{"- Tailscale connected"}
	if dnsName != "" {
		lines = append(lines,
			fmt.Sprintf("- Tailnet home:  http://%s:%d/", dnsName, port),
			fmt.Sprintf("- Tailnet admin: http://%s:%d/admin", dnsName, port),
		)
		if tailscaleServeEnabled(tailscalePath) {
			lines = append(lines,
				fmt.Sprintf("- Serve home:    https://%s/", dnsName),
				fmt.Sprintf("- Serve admin:   https://%s/admin", dnsName),
			)
		}
	}
	for _, ip := range status.Self.TailscaleIPs {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		lines = append(lines,
			fmt.Sprintf("- Tail IP home:  http://%s:%d/", ip, port),
			fmt.Sprintf("- Tail IP admin: http://%s:%d/admin", ip, port),
		)
		break
	}
	return lines
}

func tailscaleServeEnabled(tailscalePath string) bool {
	out, err := exec.Command(tailscalePath, "serve", "status").CombinedOutput()
	if err != nil {
		return false
	}
	return !strings.Contains(string(out), "No serve config")
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
