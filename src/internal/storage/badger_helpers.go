package storage

import (
	"encoding/json"
	"log"

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

func openBadger(path string) (*badger.DB, error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil
	return badger.Open(opts)
}

// openBadgerInMemory opens an ephemeral Badger instance (no disk I/O).
// Used by tests and optional dev mode via ANDB_BADGER_INMEMORY=true.
func openBadgerInMemory() (*badger.DB, error) {
	opts := badger.DefaultOptions("")
	opts.InMemory = true
	opts.Logger = nil
	return badger.Open(opts)
}
