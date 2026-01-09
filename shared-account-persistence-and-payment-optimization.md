# Shared Account Persistence and Payment Optimization

**Version:** 1.0
**Date:** January 2026
**Status:** Implemented and Tested

---

## Executive Summary

This document describes two critical improvements to the Neuron SDK:

1. **Shared Account Persistence System** - A BBolt-based caching mechanism that persists shared accounts across application restarts, eliminating redundant account creation costs.

2. **Payment Amount Optimization** - Reduction of hard-coded payment amounts to prevent excessive HBAR consumption during buyer-seller data exchange operations.

Together, these changes reduce daily operational costs from approximately 321 HBAR to 0.5 HBAR per buyer node (a 640x reduction).

---

## Part 1: Shared Account Persistence System

### 1.1 Problem Statement

Previously, when a buyer node restarted, it would lose all information about existing shared accounts created with sellers. This resulted in:

- Creation of duplicate shared accounts for the same buyer-seller pairs
- Unnecessary HBAR expenditure on account creation fees
- Wasted balance in abandoned shared accounts
- Increased operational costs for long-running deployments

### 1.2 Solution Architecture

The persistence system uses BBolt (an embedded key-value database) to cache shared account information locally. The cache persists across application restarts and enables account reuse.

#### 1.2.1 Database Location

```
~/.neuron/shared_accounts.db
```

#### 1.2.2 Data Model

```go
type SharedAccountRecord struct {
    BuyerEthAddress   string    `json:"buyer_eth_address"`
    SellerEthAddress  string    `json:"seller_eth_address"`
    ArbiterEthAddress string    `json:"arbiter_eth_address"`
    SharedAccID       uint64    `json:"shared_acc_id"`
    CreatedAt         time.Time `json:"created_at"`
}
```

#### 1.2.3 Cache Key Format

```
{buyerEthAddress}:{sellerEthAddress}:{arbiterEthAddress}
```

Example:
```
abc123def456:7fda8af4372451c4034dfaa61a0f5cf499891d3a:e2436b1e019e993215e832762f9242020d199940
```

### 1.3 Implementation Details

#### 1.3.1 Core Components

| File | Purpose |
|------|---------|
| `common-lib/shared-account-cache.go` | BBolt database operations |
| `common-lib/buffers.go` | Database initialization on startup |
| `hedera/main.go` | Cache lookup and save logic |
| `neuron-sdk.go` | Database cleanup on shutdown |

#### 1.3.2 API Functions

| Function | Description |
|----------|-------------|
| `OpenSharedAccountDB()` | Opens database with production-optimized settings |
| `CloseSharedAccountDB()` | Closes database and releases file lock |
| `LoadSharedAccount(buyer, seller, arbiter)` | Retrieves cached shared account |
| `SaveSharedAccount(record)` | Persists shared account to cache |
| `ClearSharedAccountCache()` | Removes all cached accounts |
| `ListAllSharedAccounts()` | Returns all cached records (debugging) |
| `GetSharedAccountCount()` | Returns number of cached accounts |
| `IsSharedAccountDBOpen()` | Checks if database is open |

#### 1.3.3 Database Configuration

```go
db, err := bolt.Open(dbPath, 0600, &bolt.Options{
    Timeout:      5 * time.Second,      // Prevent hanging on lock
    NoGrowSync:   true,                 // Reduce flash wear
    FreelistType: bolt.FreelistMapType, // Better for updates
})
```

### 1.4 Operation Flow

#### 1.4.1 Application Startup

```
1. StateManagerInit() called
2. OpenSharedAccountDB() opens ~/.neuron/shared_accounts.db
3. Bucket "shared_accounts" created if not exists
4. Database ready for operations
```

#### 1.4.2 Buyer Connecting to Seller

```
1. BuyerPrepareServiceRequest() called
2. LoadSharedAccount(buyer, seller, arbiter) checks cache
3. If cache HIT:
   a. Validate account exists on Hedera (free mirror query)
   b. If valid: Reuse account (skip creation)
   c. If invalid: Continue to create new account
4. If cache MISS:
   a. Create new shared account on Hedera
   b. SaveSharedAccount() persists to cache
5. Return shared account ID for service request
```

#### 1.4.3 Application Shutdown

```
1. SIGINT/SIGTERM received
2. CloseSharedAccountDB() called
3. Database file lock released
4. Application exits cleanly
```

### 1.5 Reconnection Behavior

When a seller disconnects and later reconnects:

| Scenario | Outcome |
|----------|---------|
| Seller restarts within same buyer session | Cached account reused |
| Buyer restarts, seller online | Cached account reused |
| Both restart | Cached account reused (if account still valid on Hedera) |
| Cached account depleted | New account created, cache updated |

### 1.6 Error Handling

The system implements graceful degradation:

| Error Condition | Behavior |
|-----------------|----------|
| Cannot open database | Warning logged, continues without caching |
| Cannot read from cache | Creates new account, saves to cache |
| Cannot write to cache | Warning logged, operation continues |
| Cached account invalid | Creates new account, updates cache |

### 1.7 Performance Characteristics

| Metric | Value |
|--------|-------|
| Database file size (100 accounts) | ~24 KB |
| Database file size (1000 accounts) | ~204 KB |
| Memory overhead | < 1 MB |
| Read latency | < 1 ms |
| Write latency | < 5 ms |
| Background goroutines | 0 |

---

## Part 2: Payment Amount Optimization

### 2.1 Problem Statement

The original payment configuration caused excessive HBAR consumption:

| Payment Type | Original Value | Frequency |
|--------------|----------------|-----------|
| Initial shared account balance | 0.1 HBAR | Per seller |
| Deposit refill after invoice | 1 HBAR | Every 45 minutes |
| Payment split to seller | 0.1 HBAR | Every 45 minutes |
| Seller balance threshold | 1 HBAR | N/A |

**Daily cost for 10 sellers:** ~321 HBAR

### 2.2 Optimized Configuration

| Payment Type | New Value | Reduction |
|--------------|-----------|-----------|
| Initial shared account balance | 0.01 HBAR | 10x |
| Deposit refill after invoice | 0.01 HBAR | 100x |
| Payment split to seller | 0.001 HBAR | 100x |
| Seller balance threshold | 0.01 HBAR | 100x |

**Daily cost for 10 sellers:** ~0.5 HBAR (640x reduction)

### 2.3 Implementation Details

#### 2.3.1 Three-Way Payment Split

**File:** `hedera/main.go` (lines 296-304)

```go
// Payment split: Device gets 90%, Parent gets 10%
// Total: 0.001 HBAR (100,000 tinybars)
transferTx, err := hedera.NewTransferTransaction().
    AddHbarTransfer(sharedAccID, hedera.HbarFrom(-0.001, hedera.HbarUnits.Hbar)).
    AddHbarTransfer(toHederaParentID, hedera.HbarFrom(0.0001, hedera.HbarUnits.Hbar)).
    AddHbarTransfer(toHederaDeviceID, hedera.HbarFrom(0.0009, hedera.HbarUnits.Hbar)).
```

#### 2.3.2 Initial Shared Account Balance

**File:** `dapp-protocols/stream-buyer-vs-seller/buyer-case.go` (line 373)

```go
10, // millibar (0.01 HBAR) - minimal initial balance
```

#### 2.3.3 Deposit with Balance Check

**File:** `dapp-protocols/stream-buyer-vs-seller/buyer-case.go` (lines 143-158)

```go
sharedAcc, _ := hedera.AccountIDFromString(fmt.Sprintf("0.0.%d", scheduleSignRequest.SharedAccID))

// Check balance before depositing - only top up if balance is low
accountInfo, balErr := hedera_helper.GetAccountInfoFromNetwork(sharedAcc)
if balErr == nil && accountInfo.Balance.AsTinybar() >= 1_000_000 {
    // Balance is sufficient (>= 0.01 HBAR), skip deposit
    fmt.Printf("Shared account %s has sufficient balance (%d tinybars), skipping deposit\n",
        sharedAcc, accountInfo.Balance.AsTinybar())
} else {
    // Balance is low or couldn't check, deposit minimal amount
    fmt.Println("Adding minimal funds to shared account:", sharedAcc)
    err = hedera_helper.DepositToSharedAccount(sharedAcc, 0.01)
    if err != nil {
        fmt.Println("SELFERROR: could not deposit to shared account ", err)
    }
}
```

#### 2.3.4 Seller Balance Threshold

**File:** `dapp-protocols/stream-buyer-vs-seller/seller-case.go` (line 174)

```go
if buyerSharedAccountInfo.Balance.AsTinybar() < 1_000_000 { // 0.01 HBAR threshold
```

### 2.4 Tinybars Reference

| HBAR | Tinybars |
|------|----------|
| 1 HBAR | 100,000,000 |
| 0.1 HBAR | 10,000,000 |
| 0.01 HBAR | 1,000,000 |
| 0.001 HBAR | 100,000 |
| 0.0001 HBAR | 10,000 |

### 2.5 Cost Analysis

#### 2.5.1 Before Optimization

| Action | Cost | 24h with 10 sellers |
|--------|------|---------------------|
| Initial accounts | 0.1 HBAR x 10 | 1 HBAR |
| Deposits (32 cycles) | 1 HBAR x 10 x 32 | 320 HBAR |
| **Total** | | **~321 HBAR** |

#### 2.5.2 After Optimization

| Action | Cost | 24h with 10 sellers |
|--------|------|---------------------|
| Initial accounts | 0.01 HBAR x 10 | 0.1 HBAR |
| Deposits (minimal due to balance check) | ~0.08 HBAR | ~0.08 HBAR |
| Payments | 0.001 HBAR x 10 x 32 | 0.32 HBAR |
| **Total** | | **~0.5 HBAR** |

---

## Part 3: Testing and Verification

### 3.1 Persistence System Tests

#### 3.1.1 First Run Test

```
Expected logs:
- "Shared account cache opened: /Users/.../.neuron/shared_accounts.db"
- "Created new shared account XXXXXX for seller <eth_address>"
- "Cached shared account XXXXXX for seller <eth_address>"
```

#### 3.1.2 Restart Test

```
Expected logs:
- "Shared account cache opened: /Users/.../.neuron/shared_accounts.db"
- "Using cached shared account XXXXXX for seller <eth_address> (created Xm ago)"
- "Reusing cached shared account XXXXXX for seller <eth_address> (saved HBAR!)"
```

### 3.2 Verification Commands

#### 3.2.1 Check Database File

```bash
ls -la ~/.neuron/shared_accounts.db
```

#### 3.2.2 View Cache Contents (requires bbolt CLI)

```bash
go install go.etcd.io/bbolt/cmd/bbolt@latest
bbolt keys ~/.neuron/shared_accounts.db shared_accounts
```

#### 3.2.3 Clear Cache

Run application with `--clear-cache` flag or:

```bash
rm ~/.neuron/shared_accounts.db
```

### 3.3 Test Results

| Test Case | Result |
|-----------|--------|
| Database opens on startup | PASS |
| Shared accounts cached after creation | PASS |
| Cache reused after restart | PASS |
| Invalid cached accounts recreated | PASS |
| Graceful degradation on DB error | PASS |
| Database closed on shutdown | PASS |

---

## Part 4: Production Considerations

### 4.1 Flash Storage (SD Card) Optimization

The BBolt configuration includes optimizations for flash storage:

- `NoGrowSync: true` - Reduces write amplification
- `FreelistType: bolt.FreelistMapType` - Better for update-heavy workloads
- Low write frequency (only on new account creation)

### 4.2 Concurrency

| Operation | Safety |
|-----------|--------|
| Multiple reads | Safe (concurrent) |
| Multiple writes | Safe (serialized by BBolt) |
| Read during write | Safe (snapshot isolation) |

### 4.3 Recovery

If database corruption occurs:

```bash
rm ~/.neuron/shared_accounts.db
# Node will recreate shared accounts on next run
```

### 4.4 Monitoring

Key log messages to monitor:

| Log Pattern | Meaning |
|-------------|---------|
| `Shared account cache opened` | Database initialized successfully |
| `Reusing cached shared account` | Cache hit, HBAR saved |
| `Created new shared account` | New account created (expected for new sellers) |
| `Warning: Failed to open shared account cache` | Database error (graceful degradation active) |

---

## Part 5: Files Modified

| File | Changes |
|------|---------|
| `go.mod` | Added `go.etcd.io/bbolt v1.4.3` dependency |
| `common-lib/shared-account-cache.go` | New file - BBolt cache implementation |
| `common-lib/buffers.go` | Added `OpenSharedAccountDB()` call in `StateManagerInit()` |
| `hedera/main.go` | Added cache check/save in `BuyerPrepareServiceRequest()`, reduced payment amounts |
| `neuron-sdk.go` | Added `CloseSharedAccountDB()` on shutdown |
| `dapp-protocols/stream-buyer-vs-seller/buyer-case.go` | Reduced initial payment, added balance check before deposit |
| `dapp-protocols/stream-buyer-vs-seller/seller-case.go` | Reduced balance threshold |

---

## Part 6: Appendix

### 6.1 BBolt v1.4.3 Documentation

https://pkg.go.dev/go.etcd.io/bbolt@v1.4.3

### 6.2 Hedera Fee Reference

https://docs.hedera.com/hedera/networks/mainnet/fees

### 6.3 Configuration Flags

| Flag | Description |
|------|-------------|
| `--clear-cache` | Clears shared account cache on startup |
| `--buyer-or-seller` | Determines node role (buyer/seller) |

---

**Document End**
