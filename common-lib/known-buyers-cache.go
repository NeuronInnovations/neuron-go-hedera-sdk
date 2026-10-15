package commonlib

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// known-buyers-cache.go lets a SELLER survive a Hedera/mirror outage and reboots
// without losing the buyers it already serves. It persists, in the SAME BBolt
// database used for shared accounts (see shared-account-cache.go), enough to keep
// streaming ADS-B to a buyer purely from disk:
//
//   - the buyer's libp2p addresses (so we can re-dial without a fresh topic
//     serviceRequest, which would require Hedera), and
//   - our own three topic numbers (so we can resume listening/heartbeating on
//     reboot without a contract read).
//
// The "lease" model (see KnownBuyer.LastSignOfLife): the seller keeps serving a
// buyer even when payment stops, and only gives up after a long silence with NO
// sign of life from the buyer (a fresh request, a top-up of the shared account,
// etc.). This needs ZERO new wire protocol, so it is fully backwards compatible
// with old buyers and old sellers.
//
// All functions degrade gracefully when the DB is closed (ErrDatabaseNotOpen),
// exactly like the shared-account cache: the seller then behaves as it did
// before this feature (in-memory only).

const (
	bucketKnownBuyers = "known_buyers"
	bucketSelf        = "self"
	selfTopicsKey     = "self_topics"

	// KnownBuyerLeaseTTL is how long a buyer may stay serve-able with no sign of
	// life before the seller gives up on it. Matches the existing 5-day reconnect
	// give-up (connection.go ErrGiveUpReconnect) so the two agree.
	KnownBuyerLeaseTTL = 5 * 24 * time.Hour
)

// KnownBuyer is everything the seller needs to keep (or resume) serving a buyer
// without talking to Hedera.
type KnownBuyer struct {
	BuyerEthAddress string    `json:"buyer_eth_address"`
	BuyerPublicKey  string    `json:"buyer_public_key"` // hedera pub key -> peer ID for dialing
	Multiaddrs      []string  `json:"multiaddrs"`       // buyer's reachable libp2p addresses
	BuyerStdInTopic uint64    `json:"buyer_stdin_topic"`
	SharedAccID     uint64    `json:"shared_acc_id"`
	Serve           bool      `json:"serve"`                // lease active: keep dialing/streaming
	LastSignOfLife  time.Time `json:"last_sign_of_life"`    // lease renewal stamp
	LastBalanceTiny int64     `json:"last_balance_tinybar"` // for detecting shared-account top-ups
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// LeaseExpired reports whether the buyer has been silent past the TTL and should
// be given up on. A zero LastSignOfLife is treated as "just seen" so a freshly
// saved record is never immediately expired.
func (b *KnownBuyer) LeaseExpired(now time.Time) bool {
	if b.LastSignOfLife.IsZero() {
		return false
	}
	return now.Sub(b.LastSignOfLife) > KnownBuyerLeaseTTL
}

// SelfTopics are our own three HCS topic numbers, persisted once so a rebooting
// seller can resume listening/heartbeating without a contract read.
type SelfTopics struct {
	StdOutTopic uint64    `json:"stdout_topic"`
	StdInTopic  uint64    `json:"stdin_topic"`
	StdErrTopic uint64    `json:"stderr_topic"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func normalizeEvm(evm string) string {
	s := strings.TrimSpace(strings.ToLower(evm))
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return ""
	}
	return "0x" + s
}

// SaveKnownBuyer upserts a buyer record. CreatedAt is preserved across updates.
// Graceful no-op (ErrDatabaseNotOpen) when the DB is closed.
func SaveKnownBuyer(rec *KnownBuyer) error {
	if sharedAccountDB == nil {
		return ErrDatabaseNotOpen
	}
	key := normalizeEvm(rec.BuyerEthAddress)
	if key == "" {
		return fmt.Errorf("known buyer: empty eth address")
	}
	rec.BuyerEthAddress = key
	now := time.Now()
	rec.UpdatedAt = now

	return sharedAccountDB.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucketKnownBuyers))
		if err != nil {
			return err
		}
		// Preserve CreatedAt if a record already exists.
		if rec.CreatedAt.IsZero() {
			if existing := b.Get([]byte(key)); existing != nil {
				var prev KnownBuyer
				if json.Unmarshal(existing, &prev) == nil && !prev.CreatedAt.IsZero() {
					rec.CreatedAt = prev.CreatedAt
				}
			}
			if rec.CreatedAt.IsZero() {
				rec.CreatedAt = now
			}
		}
		data, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("known buyer: marshal: %w", err)
		}
		return b.Put([]byte(key), data)
	})
}

// LoadKnownBuyer returns the cached record for a buyer EVM address, or
// ErrAccountNotFound if absent.
func LoadKnownBuyer(buyerEth string) (*KnownBuyer, error) {
	if sharedAccountDB == nil {
		return nil, ErrDatabaseNotOpen
	}
	key := normalizeEvm(buyerEth)
	var rec KnownBuyer
	err := sharedAccountDB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketKnownBuyers))
		if b == nil {
			return ErrAccountNotFound
		}
		data := b.Get([]byte(key))
		if data == nil {
			return ErrAccountNotFound
		}
		return json.Unmarshal(data, &rec)
	})
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// ListServableBuyers returns all buyers whose lease is still active (Serve==true
// and not past the TTL). Expired or non-serve records are skipped. Best-effort:
// corrupted entries are ignored.
func ListServableBuyers() ([]KnownBuyer, error) {
	if sharedAccountDB == nil {
		return nil, ErrDatabaseNotOpen
	}
	now := time.Now()
	var out []KnownBuyer
	err := sharedAccountDB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketKnownBuyers))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var rec KnownBuyer
			if json.Unmarshal(v, &rec) != nil {
				return nil // skip corrupted
			}
			if rec.Serve && !rec.LeaseExpired(now) {
				out = append(out, rec)
			}
			return nil
		})
	})
	return out, err
}

// GCExpiredKnownBuyers deletes buyers whose lease has expired (silent past the
// TTL) and returns the deleted records so the caller can drop their in-memory
// buffers too. Does a read pass first and only opens a write transaction when
// there is actually something to delete.
func GCExpiredKnownBuyers() ([]KnownBuyer, error) {
	if sharedAccountDB == nil {
		return nil, ErrDatabaseNotOpen
	}
	now := time.Now()
	var expired []KnownBuyer
	err := sharedAccountDB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketKnownBuyers))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var rec KnownBuyer
			if json.Unmarshal(v, &rec) == nil && rec.LeaseExpired(now) {
				expired = append(expired, rec)
			}
			return nil
		})
	})
	if err != nil || len(expired) == 0 {
		return nil, err
	}
	err = sharedAccountDB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketKnownBuyers))
		if b == nil {
			return nil
		}
		for _, rec := range expired {
			if derr := b.Delete([]byte(normalizeEvm(rec.BuyerEthAddress))); derr != nil {
				return derr
			}
		}
		return nil
	})
	return expired, err
}

// StampKnownBuyerSignOfLife renews the lease for a buyer: it sets Serve=true and
// LastSignOfLife=now. Called whenever the buyer shows a sign of life (a fresh
// request, a topic message, a shared-account top-up). No-op if the buyer is not
// yet known (we only stamp buyers we already persisted on connect).
func StampKnownBuyerSignOfLife(buyerEth string) error {
	return mutateKnownBuyer(buyerEth, func(rec *KnownBuyer) {
		rec.Serve = true
		rec.LastSignOfLife = time.Now()
	})
}

// SetKnownBuyerServe flips the serve flag (e.g. false to stop serving). No-op if
// the buyer is unknown.
func SetKnownBuyerServe(buyerEth string, serve bool) error {
	return mutateKnownBuyer(buyerEth, func(rec *KnownBuyer) { rec.Serve = serve })
}

// mutateKnownBuyer applies fn to an existing record in a single transaction.
// Returns ErrAccountNotFound if the buyer is unknown (so callers can ignore it).
func mutateKnownBuyer(buyerEth string, fn func(*KnownBuyer)) error {
	if sharedAccountDB == nil {
		return ErrDatabaseNotOpen
	}
	key := normalizeEvm(buyerEth)
	if key == "" {
		return fmt.Errorf("known buyer: empty eth address")
	}
	return sharedAccountDB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketKnownBuyers))
		if b == nil {
			return ErrAccountNotFound
		}
		data := b.Get([]byte(key))
		if data == nil {
			return ErrAccountNotFound
		}
		var rec KnownBuyer
		if err := json.Unmarshal(data, &rec); err != nil {
			return err
		}
		fn(&rec)
		rec.UpdatedAt = time.Now()
		out, err := json.Marshal(&rec)
		if err != nil {
			return err
		}
		return b.Put([]byte(key), out)
	})
}

// DeleteKnownBuyer removes a buyer from the cache (lease given up).
func DeleteKnownBuyer(buyerEth string) error {
	if sharedAccountDB == nil {
		return ErrDatabaseNotOpen
	}
	key := normalizeEvm(buyerEth)
	return sharedAccountDB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketKnownBuyers))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

// SaveSelfTopics persists our own three topic numbers (idempotent overwrite).
func SaveSelfTopics(stdOut, stdIn, stdErr uint64) error {
	if sharedAccountDB == nil {
		return ErrDatabaseNotOpen
	}
	rec := SelfTopics{StdOutTopic: stdOut, StdInTopic: stdIn, StdErrTopic: stdErr, UpdatedAt: time.Now()}
	data, err := json.Marshal(&rec)
	if err != nil {
		return err
	}
	return sharedAccountDB.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucketSelf))
		if err != nil {
			return err
		}
		return b.Put([]byte(selfTopicsKey), data)
	})
}

// LoadSelfTopics returns our persisted topic numbers, or ErrAccountNotFound if
// none were saved yet.
func LoadSelfTopics() (*SelfTopics, error) {
	if sharedAccountDB == nil {
		return nil, ErrDatabaseNotOpen
	}
	var rec SelfTopics
	err := sharedAccountDB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSelf))
		if b == nil {
			return ErrAccountNotFound
		}
		data := b.Get([]byte(selfTopicsKey))
		if data == nil {
			return ErrAccountNotFound
		}
		return json.Unmarshal(data, &rec)
	})
	if err != nil {
		return nil, err
	}
	return &rec, nil
}
