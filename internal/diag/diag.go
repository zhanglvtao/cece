package diag

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	path  = "/Users/bytedance/cece/.cece/diag.log"
	once  sync.Once
)

func Log(format string, args ...any) {
	once.Do(func() { _ = os.WriteFile(path, []byte(""), 0644) })
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}
