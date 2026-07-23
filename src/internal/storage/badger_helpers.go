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

func badgerSetJSONCounted(db *badger.DB, key []byte, counterName string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("[badger] marshal json failed key=%q err=%v", string(key), err)
		return err
	}
	return db.Update(func(txn *badger.Txn) error {
		exists, err := badgerTxnKeyExists(txn, key)
		if err != nil {
			return err
		}
		if err := txn.Set(key, b); err != nil {
			log.Printf("[badger] txn.Set failed key=%q err=%v", string(key), err)
			return err
		}
		if !exists {
			return badgerAddCounterTxn(txn, counterName, 1)
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

func badgerDeleteCounted(db *badger.DB, key []byte, counterName string) error {
	return db.Update(func(txn *badger.Txn) error {
		exists, err := badgerTxnKeyExists(txn, key)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		if err := txn.Delete(key); err != nil {
			return err
		}
		return badgerAddCounterTxn(txn, counterName, -1)
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

func badgerCounterValue(db *badger.DB, counterName string, fallbackPrefix string) int {
	count, found := badgerReadCounter(db, counterName)
	if found {
		return count
	}
	count = badgerCountPrefix(db, fallbackPrefix)
	_ = badgerWriteCounter(db, counterName, count)
	return count
}

func badgerReadCounter(db *badger.DB, counterName string) (int, bool) {
	var count int
	found := false
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerCounterKey(counterName))
		if err == badger.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &count)
		})
	})
	return count, found && err == nil
}

func badgerWriteCounter(db *badger.DB, counterName string, count int) error {
	return db.Update(func(txn *badger.Txn) error {
		b, err := json.Marshal(count)
		if err != nil {
			return err
		}
		return txn.Set(badgerCounterKey(counterName), b)
	})
}

func badgerAddCounterTxn(txn *badger.Txn, counterName string, delta int) error {
	count := 0
	item, err := txn.Get(badgerCounterKey(counterName))
	if err == nil {
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &count)
		}); err != nil {
			return err
		}
	} else if err != badger.ErrKeyNotFound {
		return err
	}
	count += delta
	if count < 0 {
		count = 0
	}
	b, err := json.Marshal(count)
	if err != nil {
		return err
	}
	return txn.Set(badgerCounterKey(counterName), b)
}

func badgerTxnKeyExists(txn *badger.Txn, key []byte) (bool, error) {
	_, err := txn.Get(key)
	if err == badger.ErrKeyNotFound {
		return false, nil
	}
	return err == nil, err
}

func badgerCounterKey(counterName string) []byte {
	return []byte(kpCount + counterName)
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
