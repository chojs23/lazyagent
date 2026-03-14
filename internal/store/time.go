package store

import "time"

func nowMillis() int64 {
	return time.Now().UnixMilli()
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
