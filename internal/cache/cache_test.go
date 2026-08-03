package cache

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestStorePersistsAllIndexesAndFreshnessStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "reddit.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	freshURL := "https://example.test/fresh"
	if err := store.Save(freshURL, 200, []byte(`{"ok":true}`), "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if raw, ok, err := store.Fresh(freshURL, time.Now()); err != nil || !ok || string(raw) != `{"ok":true}` {
		t.Fatalf("Fresh valid = %q, %v, %v", raw, ok, err)
	}
	if raw, ok, err := store.Fresh("missing", time.Now()); err != nil || ok || raw != nil {
		t.Fatalf("Fresh missing = %q, %v, %v", raw, ok, err)
	}
	for _, test := range []struct {
		name   string
		status int
		raw    []byte
		expiry time.Time
	}{
		{name: "status below success", status: 199, raw: []byte("body"), expiry: time.Now().Add(time.Hour)},
		{name: "status above success", status: 500, raw: []byte("body"), expiry: time.Now().Add(time.Hour)},
		{name: "expired", status: 200, raw: []byte("body"), expiry: time.Now().Add(-time.Hour)},
		{name: "empty body", status: 200, raw: nil, expiry: time.Now().Add(time.Hour)},
	} {
		t.Run(test.name, func(t *testing.T) {
			url := "https://example.test/" + strings.ReplaceAll(test.name, " ", "-")
			if err := store.Save(url, test.status, test.raw, "", test.expiry); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := store.Fresh(url, time.Now()); err != nil || ok {
				t.Fatalf("Fresh(%s) = ok=%v err=%v, want false", test.name, ok, err)
			}
		})
	}

	errorURL := "https://example.test/error"
	if err := store.Save(errorURL, 429, nil, "rate limited", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if status, message, retry, active, err := store.ActiveError(errorURL, time.Now()); err != nil || !active || status != 429 || message != "rate limited" || retry <= 0 {
		t.Fatalf("ActiveError = %d %q %s %v %v", status, message, retry, active, err)
	}
	if _, _, _, active, err := store.ActiveError(freshURL, time.Now()); err != nil || active {
		t.Fatalf("successful response reported as active error: active=%v err=%v", active, err)
	}
	if _, _, _, active, err := store.ActiveError("missing", time.Now()); err != nil || active {
		t.Fatalf("missing response reported as active error: active=%v err=%v", active, err)
	}
	if raw, ok, err := store.Stale(freshURL); err != nil || !ok || string(raw) != `{"ok":true}` {
		t.Fatalf("Stale valid = %q, %v, %v", raw, ok, err)
	}
	if raw, ok, err := store.Stale("missing"); err != nil || ok || raw != nil {
		t.Fatalf("Stale missing = %q, %v, %v", raw, ok, err)
	}
	if raw, ok, err := store.Stale("https://example.test/error"); err != nil || ok || len(raw) != 0 {
		t.Fatalf("Stale empty body = %q, %v, %v", raw, ok, err)
	}
}

func TestRequestSlotsAreSharedAcrossStoreReopens(t *testing.T) {
	// bbolt takes an exclusive file lock, so cross-process sharing is
	// expressed as: reserve, close, reopen, reserve — the second reservation
	// must be delayed by exactly one slot.
	path := filepath.Join(t.TempDir(), "reddit.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_000_000)
	firstSlot, err := first.ReserveRequestSlot(now, 250*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	secondSlot, err := second.ReserveRequestSlot(now.Add(10*time.Millisecond), 250*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !firstSlot.Equal(now) || secondSlot.Sub(firstSlot) != 250*time.Millisecond {
		t.Fatalf("slots = %s and %s, want 250ms spacing from %s", firstSlot, secondSlot, now)
	}
}

func TestGlobalCooldown(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "reddit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if cooldown, active, err := store.ActiveCooldown(time.Now()); err != nil || active || cooldown != 0 {
		t.Fatalf("empty cooldown = %s active=%v err=%v", cooldown, active, err)
	}
	if err := store.Save("blocked", 429, nil, "slow", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if cooldown, active, err := store.ActiveCooldown(time.Now()); err != nil || !active || cooldown <= 0 {
		t.Fatalf("active cooldown = %s active=%v err=%v", cooldown, active, err)
	}
}

func TestStoreAndQueryErrorsAreReturned(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "reddit.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := store.ActiveError("url", time.Now()); err == nil {
		t.Fatal("ActiveError unexpectedly succeeded on closed database")
	}
	if _, _, err := store.ActiveCooldown(time.Now()); err == nil {
		t.Fatal("ActiveCooldown unexpectedly succeeded on closed database")
	}
	if _, _, err := store.Fresh("url", time.Now()); err == nil {
		t.Fatal("Fresh unexpectedly succeeded on closed database")
	}
	if _, _, err := store.Stale("url"); err == nil {
		t.Fatal("Stale unexpectedly succeeded on closed database")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var nilStore *Store
	if err := nilStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (&Store{}).Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenInitializesBucketsConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reddit.db")
	start := make(chan struct{})
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			store, err := Open(path)
			if err == nil {
				err = store.Close()
			}
			errors <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenReportsDirectoryCreationFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(parent, "reddit.db")); err == nil {
		t.Fatal("Open unexpectedly succeeded below a regular file")
	}
}

func TestReadStatsAndFormatBytes(t *testing.T) {
	if got := ReadStats(filepath.Join(t.TempDir(), "missing.db")); got != (Stats{}) {
		t.Fatalf("missing stats = %#v, want zero", got)
	}
	path := filepath.Join(t.TempDir(), "reddit.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("ok", 200, []byte("ok"), "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("blocked", 403, nil, "blocked", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("expired-block", 429, nil, "expired", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("topic:v2:test", 200, []byte("[]"), "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	stats := ReadStats(path)
	if stats.Requests != 3 || stats.SizeBytes <= 0 || stats.Cooldown <= 0 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	for _, test := range []struct {
		size int64
		want string
	}{
		{size: 0, want: "0 B"},
		{size: 999, want: "999 B"},
		{size: 1000, want: "1.0 KB"},
		{size: 1_000_000, want: "1.0 MB"},
		{size: 1_000_000_000, want: "1.0 GB"},
		{size: 1_000_000_000_000, want: "1.0 TB"},
		{size: 1_000_000_000_000_000, want: "1000.0 TB"},
	} {
		if got := FormatBytes(test.size); got != test.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", test.size, got, test.want)
		}
	}
}

func TestOpenReportsInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.db")
	if err := os.WriteFile(path, []byte("this is not a bolt database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open unexpectedly succeeded on a non-bolt file")
	}
	if got := ReadStats(path); got.SizeBytes == 0 {
		t.Fatalf("ReadStats lost file size on open failure: %#v", got)
	}
}

func TestReadStatsHandlesDatabaseWithoutRequestsBucket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket([]byte("other"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	stats := ReadStats(path)
	if stats.Requests != 0 || stats.Cooldown != 0 {
		t.Fatalf("unexpected stats for database without requests bucket: %#v", stats)
	}
}
