package obs

import "syscall"

// ponytail: peak RSS, not current; current needs task_info via cgo or
// /usr/bin/ps, add if the strip should show memory going back down.
func maxRSS(ru syscall.Rusage) uint64 { return uint64(ru.Maxrss) } // bytes on darwin
