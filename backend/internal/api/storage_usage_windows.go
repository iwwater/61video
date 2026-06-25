//go:build windows

package api

import (
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/video-site/backend/internal/storageusage"
)

func localDiskStats(path string) (storageusage.DiskStats, error) {
	root := filepath.VolumeName(path) + "\\"
	if root == "\\" {
		root = path
	}
	rootPtr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return storageusage.DiskStats{}, err
	}
	var available uint64
	var total uint64
	var free uint64
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")
	r1, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&available)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)),
	)
	if r1 == 0 {
		if callErr != syscall.Errno(0) {
			return storageusage.DiskStats{}, callErr
		}
		return storageusage.DiskStats{}, syscall.EINVAL
	}
	return storageusage.DiskStats{
		AvailableBytes: int64(available),
		CapacityBytes:  int64(total),
	}, nil
}
