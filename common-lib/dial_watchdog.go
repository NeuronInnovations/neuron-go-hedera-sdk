package commonlib

import (
	"log"
	"os"
	"sync"
	"time"
)

// Seller dial watchdog.
//
// A seller is never dialed (the buyer is the public listener); the seller only
// dials out. go-libp2p's QUIC transport reuses the host's single listen socket as
// the SOURCE port for those outbound dials, so if the seller's router/NAT wedges
// the conntrack mapping for that fixed port — or libp2p's black-hole detector
// latches after enough consecutive failures — every dial fails and stays failed.
// A restart on the SAME port can't escape it (a full device reboot was observed
// not to recover a stuck seller). The cure is a restart on a FRESH port (see the
// random-port logic in neuron-sdk.go's init), which this watchdog triggers
// automatically: when the seller has been actively dialing buyers for stuckAfter
// without a single connected buyer, it exits so systemd restarts the process onto
// a new random UDP port.
var (
	dialWatchMu      sync.Mutex
	dialLastAttempt  time.Time
	dialLastHealthy  time.Time
	dialWatchStarted bool
)

// NoteSellerDialAttempt records that the seller just tried to dial a buyer. It
// gates the watchdog so it only ever restarts a seller that is actively trying —
// never an idle seller that simply has no buyers requesting it.
func NoteSellerDialAttempt() {
	dialWatchMu.Lock()
	dialLastAttempt = time.Now()
	dialWatchMu.Unlock()
}

// sellerHasConnectedBuyer reports whether at least one buyer is currently in the
// Connected state. While any buyer is connected the seller is healthy — even if a
// dial to a different buyer is failing — so the watchdog must not restart it.
func sellerHasConnectedBuyer() bool {
	nb := NodeBuffersInstance
	if nb == nil {
		return false
	}
	for _, info := range nb.GetBufferMap() {
		if info != nil && info.LibP2PState == Connected {
			return true
		}
	}
	return false
}

// StartSellerDialWatchdog launches a single background goroutine that exits the
// process (exit 1 → systemd Restart=on-failure → fresh random port) when the
// seller has been actively dialing buyers for stuckAfter without any connected
// buyer. No-op if called more than once. Safe for an idle seller: it never fires
// unless dials are actually being attempted.
func StartSellerDialWatchdog(stuckAfter time.Duration) {
	dialWatchMu.Lock()
	if dialWatchStarted {
		dialWatchMu.Unlock()
		return
	}
	dialWatchStarted = true
	dialLastHealthy = time.Now() // grace period from process start
	dialWatchMu.Unlock()

	// Only fire while the seller is actively attempting dials (a recent attempt),
	// so a seller no buyer is requesting is left alone rather than restart-looped.
	const activeWindow = 3 * time.Minute
	const checkEvery = 30 * time.Second

	go func() {
		for {
			time.Sleep(checkEvery)

			if sellerHasConnectedBuyer() {
				dialWatchMu.Lock()
				dialLastHealthy = time.Now()
				dialWatchMu.Unlock()
				continue
			}

			dialWatchMu.Lock()
			la, lh := dialLastAttempt, dialLastHealthy
			dialWatchMu.Unlock()

			if la.IsZero() {
				continue // no buyer has ever requested us; nothing to heal
			}
			if time.Since(la) <= activeWindow && time.Since(lh) >= stuckAfter {
				log.Printf("[SELLER WATCHDOG] no connected buyer for %s while actively dialing (last attempt %s ago) — exiting for a clean restart on a fresh random UDP port",
					stuckAfter, time.Since(la).Round(time.Second))
				os.Exit(1)
			}
		}
	}()
}
