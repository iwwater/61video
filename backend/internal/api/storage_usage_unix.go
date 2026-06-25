//go:build !windows

package api

import (
	"github.com/video-site/backend/internal/storageusage"
	"golang.org/x/sys/unix"
)

func localDiskStats(path string) (storageusage.DiskStats, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return storageusage.DiskStats{}, err
	}
	blockSize := uint64(stat.Bsize)
	return storageusage.DiskStats{
		AvailableBytes: int64(uint64(stat.Bavail) * blockSize),
		CapacityBytes:  int64(uint64(stat.Blocks) * blockSize),
	}, nil
}
