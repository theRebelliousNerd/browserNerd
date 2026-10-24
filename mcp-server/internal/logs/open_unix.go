//go:build unix

package logs

import "syscall"

// Nonblocking open also makes a regular-file-to-FIFO race fail without hanging.
const nonblockFlag = syscall.O_NONBLOCK
