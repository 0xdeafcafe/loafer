//go:build !darwin

package obs

import "syscall"

func maxRSS(ru syscall.Rusage) uint64 { return uint64(ru.Maxrss) * 1024 } // KiB on linux
