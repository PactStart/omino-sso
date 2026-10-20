package clock

import "time"

// Clock 抽象时间，便于测试。
type Clock interface {
	Now() time.Time
}

// System 使用系统时间。
type System struct{}

func (System) Now() time.Time { return time.Now() }

// NowMillis 返回 Unix 毫秒，全项目时间落库统一使用。
func NowMillis() int64 { return time.Now().UnixMilli() }

// Millis 把时间转换为 Unix 毫秒。
func Millis(t time.Time) int64 { return t.UnixMilli() }

// FromMillis 把 Unix 毫秒转换为时间。
func FromMillis(ms int64) time.Time { return time.UnixMilli(ms) }
