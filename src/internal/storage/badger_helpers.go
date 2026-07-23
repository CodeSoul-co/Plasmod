package storage

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

func badgerSetJSON(db *badger.DB, key []byte, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("[badger] marshal json failed key=%q err=%v", string(key), err)
		return err
	}
	return db.Update(func(txn *badger.Txn) error {
		if err := txn.Set(key, b); err != nil {
			log.Printf("[badger] txn.Set failed key=%q err=%v", string(key), err)
			return err
		}
		return nil
	})
}

func badgerGetJSON(db *badger.DB, key []byte, dest any) (bool, error) {
	var found bool
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			log.Printf("[badger] txn.Get failed key=%q err=%v", string(key), err)
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, dest)
		})
	})
	if err != nil {
		log.Printf("[badger] view failed key=%q err=%v", string(key), err)
	}
	return found, err
}

func badgerDelete(db *badger.DB, key []byte) error {
	return db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

func badgerCountPrefix(db *badger.DB, prefix string) int {
	count := 0
	_ = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		p := []byte(prefix)
		for it.Seek(p); it.ValidForPrefix(p); it.Next() {
			count++
		}
		return nil
	})
	return count
}

func openBadger(path string) (*badger.DB, error) {
	return badger.Open(badgerOptions(path))
}

func badgerOptions(path string) badger.Options {
	opts := badger.DefaultOptions(path)
	// Keep canonical JSON values out of the LSM once they exceed 1 KiB. The LSM
	// then carries compact keys and value pointers instead of repeatedly
	// compacting event payloads, without increasing Badger's memory buffers.
	opts.ValueThreshold = badgerValueThreshold(os.Getenv)
	opts.Logger = nil
	return opts
}

func badgerValueThreshold(get func(string) string) int64 {
	const defaultThreshold = int64(1 << 10)
	raw := strings.TrimSpace(get(EnvBadgerValueThreshold))
	if raw == "" {
		return defaultThreshold
	}
	threshold, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || threshold <= 0 || threshold > 1<<20 {
		return defaultThreshold
	}
	return threshold
}

// openBadgerInMemory opens an ephemeral Badger instance (no disk I/O).
// Used by tests and optional dev mode via PLASMOD_BADGER_INMEMORY=true.
func openBadgerInMemory() (*badger.DB, error) {
	opts := badger.DefaultOptions("")
	opts.InMemory = true
	opts.Logger = nil
	return badger.Open(opts)
}
