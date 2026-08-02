package cache

import (
	"bytes"
	"fmt"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
)

type Stats struct {
	Requests  int64
	SizeBytes int64
	Cooldown  time.Duration
}

func ReadStats(path string) Stats {
	info, err := os.Stat(path)
	if err != nil {
		return Stats{}
	}

	stats := Stats{SizeBytes: info.Size()}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: openTimeout})
	if err != nil {
		return stats
	}
	defer db.Close()

	var requests int64
	var expiresAt int64
	cutoff := time.Now().UnixMilli()
	if err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(requestsBucket)
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(key, value []byte) error {
			if !bytes.HasPrefix(key, topicCachePrefix) {
				requests++
			}
			status, _, expires, _, _, err := decodeRequest(value)
			if err == nil && (status == 403 || status == 429) && expires > cutoff && expires > expiresAt {
				expiresAt = expires
			}
			return nil
		})
	}); err != nil {
		return stats
	}
	stats.Requests = requests
	if expiresAt > 0 {
		stats.Cooldown = time.Until(time.UnixMilli(expiresAt))
	}
	return stats
}

func FormatBytes(size int64) string {
	if size < 1000 {
		return fmt.Sprintf("%d B", size)
	}

	value := float64(size) / 1000
	if value < 1000 {
		return fmt.Sprintf("%.1f KB", value)
	}
	value /= 1000
	if value < 1000 {
		return fmt.Sprintf("%.1f MB", value)
	}
	value /= 1000
	if value < 1000 {
		return fmt.Sprintf("%.1f GB", value)
	}
	value /= 1000
	return fmt.Sprintf("%.1f TB", value)
}
