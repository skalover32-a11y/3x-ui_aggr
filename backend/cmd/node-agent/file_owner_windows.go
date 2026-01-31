//go:build windows

package main

import "os"

func fileOwner(info os.FileInfo) (int, int) {
	return 0, 0
}
