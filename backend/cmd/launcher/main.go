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
	"sync"
	"syscall"
	"time"
)

const (
	frontendPort = 6191
	backendPort  = 6192
)

type launchMode string

const (
	modeLocalOnly     launchMode = "local-only"
	modePrivateRemote launchMode = "private-remote"
)

type serviceSpec struct {
	name    string
	port    int
	workDir string
	command string
	args    []string
	logPath string
	env     []string
}

type startedProcess struct {
	name string
	cmd  *exec.Cmd
}

type tailscaleStatus struct {
	BackendState string `json:"BackendState"`
	Self         struct {
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
	mode, err := resolveLaunchMode(os.Args[1:])
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
			env: []string{
				"VIDEO_SITE_SERVER_LISTEN=127.0.0.1:6192",
			},
		},
		{
			name:    "frontend",
			port:    frontendPort,
			workDir: repoRoot,
			command: nodePath,
			args: []string{
				filepath.Join(repoRoot, "node_modules", "vite", "bin", "vite.js"),
				"preview",
				"--host", frontendHostForMode(mode),
				"--port", fmt.Sprintf("%d", frontendPort),
			},
			logPath: filepath.Join(tmpDir, "launcher-frontend.log"),
		},
	}

	fmt.Println("61 launcher")
	fmt.Println(strings.Repeat("=", 48))
	fmt.Printf("Repo: %s\n", repoRoot)
	fmt.Printf("Logs: %s\n", tmpDir)
	fmt.Printf("Mode: %s\n", mode)
	fmt.Println("Auth: site login required")
	if mode == modePrivateRemote {
		fmt.Println("Remote access: frontend exposed on private network; backend stays loopback-only behind Vite proxy")
	} else {
		fmt.Println("Remote access: disabled (frontend bound to 127.0.0.1)")
	}
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
	if mode == modePrivateRemote {
		for _, ip := range localIPv4Addrs() {
			fmt.Printf("- LAN home:    http://%s:%d/\n", ip, frontendPort)
			fmt.Printf("- LAN admin:   http://%s:%d/admin\n", ip, frontendPort)
		}
		for _, line := range tailscaleAccessLines(frontendPort) {
			fmt.Println(line)
		}
	} else {
		fmt.Println("- Private remote mode is off. Re-run with --mode private-remote for Tailscale/LAN access.")
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
	cmd.Env = extendEnvForBackend(spec.env)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	return cmd, nil
}

// extendEnvForBackend 把"完整 Windows PATH"合并到 child env。
//
// 背景：launcher 在 explorer 双击启动时，自身拿到的 PATH 只是 explorer 透传
// 的一份精简版（去掉用户 PATH、App Paths 等）。ffmpeg / ffprobe 通常装在
// 用户 PATH 里（如 Anaconda、WinGet、Chocolatey、GamePP 捆绑），后端 spawn
// 后找不到 → 视频预览/封面生成全部失败。
//
// 修法（两层兜底）：
//  1. 先用 `cmd /c echo %PATH%` 拿 cmd.exe 看到的 PATH（含注册表合并）。
//     注意 cmd.exe 也可能继承精简 PATH，所以这一步仅作 best-effort。
//  2. 不论上面结果如何，再扫一组"常见 ffmpeg/ffprobe 安装位置"，找到就把
//     那个目录加到 PATH 前面。这覆盖 Anaconda、WinGet、GamePP 捆绑等
//     不在注册表 PATH 里的情况。
//
// 仅 Windows 起作用。
var (
	cachedFullPATH     string
	cachedFullOnce     sync.Once
	cachedExtraBinDirs []string
)

// wellKnownFFmpegDirs 是一组常见 ffmpeg/ffprobe 安装位置。launcher 在这些
// 目录里找 ffmpeg.exe / ffprobe.exe，找到就把目录加到 child PATH。
// 用户机器上没装 ffmpeg 时这个列表是空，等于无操作；找到第一个匹配就
// 提前结束。
var wellKnownFFmpegDirs = []string{
	`D:\ANACONDA\Library\bin`,
	`D:\ANACONDA\bin`,
	`C:\ProgramData\anaconda3\Library\bin`,
	`C:\ProgramData\miniconda3\Library\bin`,
	`C:\tools\Anaconda3\Library\bin`,
	// WinGet Gyan FFmpeg 安装位置（用户具体目录名随版本变）
	`C:\Users\BAi\AppData\Local\Microsoft\WinGet\Packages\Gyan.FFmpeg_Microsoft.Winget.Source_8wekyb3d8bbwe\ffmpeg-8.1.1-full_build\bin`,
	// Chocolatey 安装位置
	`C:\ProgramData\chocolatey\bin`,
	// scoop
	`C:\Users\BAi\scoop\apps\ffmpeg\current\bin`,
	`C:\Program Files\scoop\apps\ffmpeg\current\bin`,
	// GamePP 捆绑（4.4 旧版，凑合用）
	`C:\ProgramData\GamePPPublic\PCBenchmark`,
}

func extendEnvForBackend(extra []string) []string {
	base := os.Environ()
	if runtime.GOOS != "windows" {
		return append(base, extra...)
	}

	// 兜底层 1：cmd 看到的 PATH。
	cmdPath := loadWindowsFullPATH()

	// 兜底层 2：扫描 wellKnownFFmpegDirs，把含 ffmpeg.exe/ffprobe.exe 的目录加进去。
	extraBinDirs := locateFFmpegDirs()

	merged := cmdPath
	for _, d := range extraBinDirs {
		if merged == "" {
			merged = d
		} else if !containsDir(merged, d) {
			merged = d + string(filepath.ListSeparator) + merged
		}
	}

	out := make([]string, 0, len(base)+len(extra)+1)
	if merged != "" {
		out = append(out, "PATH="+merged)
	}
	for _, kv := range base {
		if strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "Path=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, extra...)
	return out
}

func containsDir(pathEnv, dir string) bool {
	for _, p := range strings.Split(pathEnv, string(filepath.ListSeparator)) {
		if strings.EqualFold(strings.TrimSpace(p), dir) {
			return true
		}
	}
	return false
}

func loadWindowsFullPATH() string {
	cachedFullOnce.Do(func() {
		// cmd.exe 在 Windows 上会按注册表 + 用户 PATH 解析完整 PATH。
		// /c "echo %PATH%" 让 cmd 直接输出最终值，避免我们用 syscall
		// 读注册表（那需要 x/sys/windows/registry 还没 vendor）。
		out, err := exec.Command("cmd", "/c", "echo", "%PATH%").Output()
		if err != nil {
			cachedFullPATH = ""
			return
		}
		cachedFullPATH = strings.TrimSpace(string(out))
	})
	return cachedFullPATH
}

// locateFFmpegDirs 扫描 wellKnownFFmpegDirs，找含 ffmpeg.exe 的目录。
// 命中即返回（不需要找全——一个就够 backend 启动）。结果按扫描顺序，
// 配合 extendEnvForBackend 把更"标准"的目录放前面。
//
// 2026-06-28 加：探测 libx264 支持才纳入。背景是 Anaconda 自带 ffmpeg
// 是 --disable-gpl 编译、不含 libx264，preview worker 用 -c:v libx264 -preset
// 时会全部失败（"Unrecognized option 'preset'"）。GamePP / WinGet / scoop 等
// 含 libx264 的版本会被优先选中。
func locateFFmpegDirs() []string {
	if cachedExtraBinDirs != nil {
		return cachedExtraBinDirs
	}
	var found []string
	var rejected []string
	for _, dir := range wellKnownFFmpegDirs {
		if dir == "" {
			continue
		}
		ffmpeg := filepath.Join(dir, "ffmpeg.exe")
		if !fileExists(ffmpeg) {
			continue
		}
		if !ffmpegSupportsLibx264(ffmpeg) {
			rejected = append(rejected, dir)
			continue
		}
		found = append(found, dir)
	}
	if len(found) == 0 && len(rejected) > 0 {
		// 一个都不支持 libx264 时，把第一个也带上，避免 launcher 把
		// PATH 截断到空 → backend 启动时 ffmpeg 直接 NotFound。失败留给
		// preview worker 自己用 RecordPreviewError 报清楚原因。
		found = append(found, rejected[0])
		fmt.Fprintf(os.Stderr,
			"[launcher] warning: no ffmpeg with libx264 found in wellKnownFFmpegDirs; "+
				"falling back to %s (preview generation will likely fail). "+
				"Install GPL ffmpeg and set preview.ffmpeg_path in config.yaml.\n",
			rejected[0])
	}
	cachedExtraBinDirs = found
	return found
}

// ffmpegSupportsLibx264 跑一次 ffmpeg -hide_banner -encoders，检查输出
// 是否含 "libx264"。不区分 D/S/V 前缀，只看名字。
//
// 探测本身 ~30-80ms（ffmpeg 启动 + 输出几百行 encoders 列表），只在
// launcher 启动时跑一次，结果缓存进 cachedExtraBinDirs。
func ffmpegSupportsLibx264(ffmpegPath string) bool {
	out, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").Output()
	if err != nil {
		// ffmpeg 自身跑不起来时（如权限、损坏 DLL），不要纳入候选。
		return false
	}
	return strings.Contains(string(out), "libx264")
}

func resolveLaunchMode(args []string) (launchMode, error) {
	mode := strings.TrimSpace(os.Getenv("VIDEO_SITE_MODE"))
	if mode == "" {
		mode = string(modeLocalOnly)
	}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "--mode" && i+1 < len(args):
			mode = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--mode="):
			mode = strings.TrimSpace(strings.TrimPrefix(arg, "--mode="))
		}
	}
	switch launchMode(strings.ToLower(mode)) {
	case modeLocalOnly:
		return modeLocalOnly, nil
	case modePrivateRemote:
		return modePrivateRemote, nil
	default:
		return "", fmt.Errorf("unsupported mode %q; use --mode local-only or --mode private-remote", mode)
	}
}

func frontendHostForMode(mode launchMode) string {
	if mode == modePrivateRemote {
		return "0.0.0.0"
	}
	return "127.0.0.1"
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
