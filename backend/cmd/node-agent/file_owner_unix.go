//go:build !windows

package main

import (
	"os"
	"syscall"
)

func fileOwner(info os.FileInfo) (int, int) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return int(stat.Uid), int(stat.Gid)
}
