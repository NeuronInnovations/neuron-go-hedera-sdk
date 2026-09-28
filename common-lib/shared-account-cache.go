package commonlib

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// SharedAccountRecord represents a cached shared account between a buyer and seller
type SharedAccountRecord struct {
	BuyerEthAddress   string    `json:"buyer_eth_address"`
	SellerEthAddress  string    `json:"seller_eth_address"`
	ArbiterEthAddress string    `json:"arbiter_eth_address"`
	SharedAccID       uint64    `json:"shared_acc_id"`
	CreatedAt         time.Time `json:"created_at"`
}

const (
	bucketSharedAccounts = "shared_accounts"
	dbFileName           = "shared_accounts.db"
)

var (
	sharedAccountDB    *bolt.DB
	ErrAccountNotFound = errors.New("shared account not found in cache")
	ErrDatabaseNotOpen = errors.New("shared account database not initialized")
)

// OpenSharedAccountDB opens the BBolt database for shared account caching.
// Path is chosen in order: NEURON_SHARED_ACCOUNT_DB (full file path), NEURON_CACHE_DIR (directory),
// then ~/.neuron, then ./.neuron (current directory). First successful open wins so the cache
// works even when HOME is unset or read-only.
// Uses production-optimized settings for flash storage (SD cards).
func OpenSharedAccountDB() error {
	var candidates []string

	// 1) Explicit full path to DB file
	if p := os.Getenv("NEURON_SHARED_ACCOUNT_DB"); p != "" {
		candidates = append(candidates, p)
	}
	// 2) Directory from env; we'll put shared_accounts.db inside it
	if d := os.Getenv("NEURON_CACHE_DIR"); d != "" {
		candidates = append(candidates, filepath.Join(d, dbFileName))
	}
	// 2b) systemd StateDirectory ($STATE_DIRECTORY when StateDirectory= is set),
	// then the conventional service state path. Gives a packaged service a
	// persistent, writable home for the cache even when HOME is absent
	// (e.g. a `useradd -r` service user) and the working dir is read-only.
	if sd := os.Getenv("STATE_DIRECTORY"); sd != "" {
		candidates = append(candidates, filepath.Join(sd, dbFileName))
	}
	candidates = append(candidates, filepath.Join("/var/lib/neuron-sdk", dbFileName))
	// 3) Home directory
	if homeDir, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(homeDir, ".neuron", dbFileName))
	}
	// 4) Current working directory (fallback when HOME is missing or read-only)
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, ".neuron", dbFileName))
	}

	opts := &bolt.Options{
		Timeout:      5 * time.Second,
		NoGrowSync:   true,
		FreelistType: bolt.FreelistMapType,
	}

	for _, dbPath := range candidates {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			log.Printf("Shared account cache: skip %s (mkdir: %v)", dbPath, err)
			continue
		}

		db, err := bolt.Open(dbPath, 0600, opts)
		if err != nil {
			log.Printf("Shared account cache: skip %s (open: %v)", dbPath, err)
			continue
		}

		err = db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte(bucketSharedAccounts))
			return err
		})
		if err != nil {
			db.Close()
			log.Printf("Shared account cache: skip %s (bucket: %v)", dbPath, err)
			continue
		}

		sharedAccountDB = db
		log.Printf("Shared account cache opened: %s", dbPath)
		return nil
	}

	return fmt.Errorf("could not open shared account cache at any path (tried %d)", len(candidates))
}

// CloseSharedAccountDB closes the database and releases the file lock
func CloseSharedAccountDB() error {
	if sharedAccountDB != nil {
		log.Println("Closing shared account cache...")
		err := sharedAccountDB.Close()
		sharedAccountDB = nil // Clear reference to prevent use-after-close
		return err
	}
	return nil
}

// LoadSharedAccount retrieves a cached shared account from BBolt (read-only transaction)
// Returns ErrAccountNotFound if no cached account exists for the given triple
func LoadSharedAccount(buyerEth, sellerEth, arbiterEth string) (*SharedAccountRecord, error) {
	if sharedAccountDB == nil {
		return nil, ErrDatabaseNotOpen
	}

	key := makeCacheKey(buyerEth, sellerEth, arbiterEth)
	var record SharedAccountRecord

	err := sharedAccountDB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSharedAccounts))
		if b == nil {
			return ErrAccountNotFound
		}

		data := b.Get([]byte(key))
		if data == nil {
			return ErrAccountNotFound
		}

		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("corrupted cache entry for %s: %w", key, err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	log.Printf("Using cached shared account %d for seller %s (created %s ago)",
		record.SharedAccID,
		sellerEth,
		time.Since(record.CreatedAt).Round(time.Second))

	return &record, nil
}

// SaveSharedAccount persists a shared account to the BBolt cache (read-write transaction)
// Returns ErrDatabaseNotOpen if database is not initialized (graceful degradation)
func SaveSharedAccount(record *SharedAccountRecord) error {
	if sharedAccountDB == nil {
		return ErrDatabaseNotOpen // Graceful degradation - caller continues without caching
	}

	if record.BuyerEthAddress == "" || record.SellerEthAddress == "" {
		return errors.New("invalid cache key: buyer or seller address is empty")
	}

	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}

	key := makeCacheKey(record.BuyerEthAddress, record.SellerEthAddress, record.ArbiterEthAddress)
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("cannot serialize shared account record: %w", err)
	}

	err = sharedAccountDB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSharedAccounts))
		if b == nil {
			return fmt.Errorf("shared_accounts bucket not found")
		}

		return b.Put([]byte(key), data)
	})

	if err != nil {
		return fmt.Errorf("cannot save shared account to cache: %w", err)
	}

	log.Printf("Cached shared account %d for seller %s", record.SharedAccID, record.SellerEthAddress)
	return nil
}

// ClearSharedAccountCache removes all cached shared accounts
// Useful for testing or when --clear-cache flag is set
func ClearSharedAccountCache() error {
	if sharedAccountDB == nil {
		return ErrDatabaseNotOpen
	}

	err := sharedAccountDB.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket([]byte(bucketSharedAccounts)); err != nil && err != bolt.ErrBucketNotFound {
			return err
		}
		_, err := tx.CreateBucket([]byte(bucketSharedAccounts))
		return err
	})

	if err != nil {
		return err
	}

	log.Println("Cleared shared account cache")
	return nil
}

// ListAllSharedAccounts returns all cached shared accounts (for debugging)
func ListAllSharedAccounts() ([]SharedAccountRecord, error) {
	if sharedAccountDB == nil {
		return nil, ErrDatabaseNotOpen
	}

	var records []SharedAccountRecord

	err := sharedAccountDB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSharedAccounts))
		if b == nil {
			return nil
		}

		return b.ForEach(func(k, v []byte) error {
			var record SharedAccountRecord
			if err := json.Unmarshal(v, &record); err != nil {
				log.Printf("Warning: skipping corrupted record %s: %v", k, err)
				return nil // Skip corrupted records
			}
			records = append(records, record)
			return nil
		})
	})

	return records, err
}

// GetSharedAccountCount returns the number of cached shared accounts
func GetSharedAccountCount() (int, error) {
	if sharedAccountDB == nil {
		return 0, ErrDatabaseNotOpen
	}

	count := 0
	err := sharedAccountDB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSharedAccounts))
		if b == nil {
			return nil
		}

		return b.ForEach(func(k, v []byte) error {
			count++
			return nil
		})
	})

	return count, err
}

// makeCacheKey creates a unique key for the buyer-seller-arbiter triple
// Format: {buyerEth}:{sellerEth}:{arbiterEth}
func makeCacheKey(buyerEth, sellerEth, arbiterEth string) string {
	return fmt.Sprintf("%s:%s:%s", buyerEth, sellerEth, arbiterEth)
}

// IsSharedAccountDBOpen returns true if the database is currently open
func IsSharedAccountDBOpen() bool {
	return sharedAccountDB != nil
}
