//go:build linux

package buildinfo

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"syscall"
)

func readRunningImage() runningImageIdentity {
	const source = "proc_self_exe"
	unavailable := func(reason string) runningImageIdentity {
		return runningImageIdentity{source: source, reason: reason}
	}
	file, err := os.Open("/proc/self/exe")
	if err != nil {
		return unavailable("running_image_open_failed: " + err.Error())
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return unavailable("running_image_stat_failed: " + err.Error())
	}
	identity, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return unavailable("running_image_file_identity_unavailable")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return unavailable("running_image_hash_failed: " + err.Error())
	}
	return runningImageIdentity{built: info.ModTime(), sha256: fmt.Sprintf("%x", hash.Sum(nil)),
		fileID: fmt.Sprintf("%d:%d", identity.Dev, identity.Ino), source: source}
}
