package cache

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ErrNotFound is returned when a cache key has no stored entry.
var ErrNotFound = errors.New("cache: entry not found")

var (
	requestsBucket     = []byte("requests")
	runtimeStateBucket = []byte("runtime_state")
	nextRequestSlotKey = []byte("next_request_at")
	topicCachePrefix   = []byte("topic:")
	allBuckets         = [][]byte{requestsBucket, runtimeStateBucket}
	openTimeout        = 5 * time.Second
)

type Store struct {
	db *bolt.DB
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: openTimeout})
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.ensureBuckets(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) ensureBuckets() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range allBuckets {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Fresh(url string, now time.Time) ([]byte, bool, error) {
	status, expiresAt, raw, _, err := s.entry(url)
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if status < 200 || status >= 300 || expiresAt <= now.UnixMilli() || len(raw) == 0 {
		return nil, false, nil
	}
	return raw, true, nil
}

func (s *Store) ActiveError(url string, now time.Time) (int, string, time.Duration, bool, error) {
	status, expiresAt, _, message, err := s.entry(url)
	if errors.Is(err, ErrNotFound) {
		return 0, "", 0, false, nil
	}
	if err != nil {
		return 0, "", 0, false, err
	}
	if status < 400 || expiresAt <= now.UnixMilli() {
		return 0, "", 0, false, nil
	}
	return status, message, time.Until(time.UnixMilli(expiresAt)), true, nil
}

func (s *Store) ActiveCooldown(now time.Time) (time.Duration, bool, error) {
	cutoff := now.UnixMilli()
	var expiresAt int64
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(requestsBucket)
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(_, value []byte) error {
			status, expires, _, _, err := decodeRequest(value)
			if err != nil {
				return nil // skip corrupt entries, the cache is disposable
			}
			if (status == 403 || status == 429) && expires > cutoff && expires > expiresAt {
				expiresAt = expires
			}
			return nil
		})
	})
	if err != nil || expiresAt == 0 {
		return 0, false, err
	}
	return time.Until(time.UnixMilli(expiresAt)), true, nil
}

func (s *Store) Stale(url string) ([]byte, bool, error) {
	_, _, raw, _, err := s.entry(url)
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(raw) == 0 {
		return nil, false, nil
	}
	return raw, true, nil
}

func (s *Store) entry(url string) (status int, expiresAt int64, raw []byte, errText string, err error) {
	var value []byte
	err = s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(requestsBucket)
		if bucket == nil {
			return nil
		}
		value = bytes.Clone(bucket.Get([]byte(url)))
		return nil
	})
	if err != nil {
		return 0, 0, nil, "", err
	}
	if value == nil {
		return 0, 0, nil, "", ErrNotFound
	}
	return decodeRequest(value)
}

func (s *Store) Save(url string, status int, raw []byte, errText string, expiresAt time.Time) error {
	value := encodeRequest(status, expiresAt.UnixMilli(), raw, errText)
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(requestsBucket).Put([]byte(url), value)
	})
}

// ReserveRequestSlot atomically allocates a send time shared by every process
// using this cache. A process that exits after reserving merely leaves one
// conservative delay-sized gap. bbolt serializes writers, so the
// read-modify-write inside a single Update transaction is race-free.
func (s *Store) ReserveRequestSlot(now time.Time, delay time.Duration) (time.Time, error) {
	if delay < 0 {
		delay = 0
	}
	nowMS := now.UnixMilli()
	delayMS := delay.Milliseconds()
	var scheduledMS int64
	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(runtimeStateBucket)
		var previous int64
		if value := bucket.Get(nextRequestSlotKey); len(value) == 8 {
			previous = int64(binary.BigEndian.Uint64(value))
		}
		scheduledMS = max(previous, nowMS)
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], uint64(scheduledMS+delayMS))
		return bucket.Put(nextRequestSlotKey, encoded[:])
	})
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(scheduledMS), nil
}

type requestRecord struct {
	Status    int    `json:"status"`
	ExpiresAt int64  `json:"expires_at"`
	Raw       []byte `json:"raw,omitempty"`
	ErrorText string `json:"error,omitempty"`
}

func encodeRequest(status int, expiresAt int64, raw []byte, errText string) []byte {
	value, _ := json.Marshal(requestRecord{Status: status, ExpiresAt: expiresAt, Raw: raw, ErrorText: errText})
	return value
}

func decodeRequest(value []byte) (status int, expiresAt int64, raw []byte, errText string, err error) {
	var record requestRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return 0, 0, nil, "", errors.New("cache: corrupt request record")
	}
	return record.Status, record.ExpiresAt, record.Raw, record.ErrorText, nil
}
