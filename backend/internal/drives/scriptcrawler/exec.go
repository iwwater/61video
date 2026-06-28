package scriptcrawler

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func resolveScriptCommand(pythonPath, scriptPath string, extraArgs ...string) (string, []string, error) {
	scriptPath = strings.TrimSpace(scriptPath)
	if scriptPath == "" {
		return "", nil, fmt.Errorf("script path is required")
	}
	if canExecuteScriptDirectly(scriptPath) {
		return scriptPath, append([]string{}, extraArgs...), nil
	}
	runner := strings.TrimSpace(pythonPath)
	if runner == "" {
		runner = defaultScriptRunner()
	}
	return runner, append([]string{scriptPath}, extraArgs...), nil
}

func defaultScriptRunner() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

func canExecuteScriptDirectly(scriptPath string) bool {
	info, err := os.Stat(scriptPath)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(scriptPath)) {
		case ".exe", ".cmd", ".bat", ".com":
			return true
		default:
			return false
		}
	}
	return info.Mode()&0o111 != 0
}
