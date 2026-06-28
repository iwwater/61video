package scriptcrawler

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writePlatformScript(t *testing.T, dir, baseName, unixBody, windowsBody string) string {
	t.Helper()
	ext := ".sh"
	body := "#!/bin/sh\n" + unixBody
	if runtime.GOOS == "windows" {
		ext = ".cmd"
		body = "@echo off\r\n" + windowsBody
	}
	path := filepath.Join(dir, baseName+ext)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write platform script: %v", err)
	}
	return path
}

func writeHelperWrapperScript(t *testing.T, dir string) string {
	t.Helper()
	return writePlatformScript(
		t,
		dir,
		"helper-wrapper",
		fmt.Sprintf("exec %q -test.run=TestScriptCrawlerHelperProcess -- \"$@\"\n", os.Args[0]),
		fmt.Sprintf("\"%s\" -test.run=TestScriptCrawlerHelperProcess -- %%*\r\n", os.Args[0]),
	)
}
