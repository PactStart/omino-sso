package id

import (
	"crypto/rand"
	"sync"

	"github.com/oklog/ulid/v2"
)

var (
	mu      sync.Mutex
	entropy = ulid.Monotonic(rand.Reader, 0)
)

// New 生成 26 位 ULID 字符串。
func New() string {
	mu.Lock()
	defer mu.Unlock()
	return ulid.MustNew(ulid.Now(), entropy).String()
}
