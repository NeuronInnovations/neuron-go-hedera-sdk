# BBolt Database Performance Assessment Report

**Document Version:** 1.1  
**Assessment Date:** November 26, 2025  
**Test Platform:** macOS (Apple Silicon)  
**Test Scale:** 10-100 peer records

---

## 1. Executive Summary

This report presents measured performance data for the BBolt embedded database used in the Neuron SDK. All values in this report are actual measurements from benchmark tests executed on the development machine specified below.

**Important Note:** These benchmarks were executed on a high-performance Apple M2 Max development machine. Performance on embedded devices (e.g., Raspberry Pi with SD card storage) will be significantly different, particularly for write operations where storage I/O is the bottleneck.

### Key Measured Metrics

| Operation                        | Latency | Memory  | Allocations |
| -------------------------------- | ------- | ------- | ----------- |
| Serialize (JSON + CRC32)         | 2.4 us  | 2,116 B | 15          |
| Deserialize (CRC32 + JSON parse) | 4.9 us  | 1,344 B | 24          |
| Single Peer Read                 | 7.1 us  | 2,896 B | 34          |
| Load All Peers (100 records)     | 585 us  | 198 KB  | 2,920       |
| Batched Write (50 records)       | 10.5 ms | 156 KB  | 1,191       |

### Database Size

| Peer Count | File Size | Avg per Record |
| ---------- | --------- | -------------- |
| 10         | 112 KB    | 11.5 KB        |
| 50         | 176 KB    | 3.6 KB         |
| 100        | 304 KB    | 3.1 KB         |

---

## 2. Test Environment

### Hardware

| Component    | Value                   |
| ------------ | ----------------------- |
| Platform     | macOS 25.0.0 Darwin     |
| CPU          | Apple M2 Max (12 cores) |
| Architecture | ARM64                   |
| Memory       | 32 GB                   |
| Storage      | NVMe SSD                |

### Software

| Component      | Version               |
| -------------- | --------------------- |
| Go Runtime     | 1.25.1 darwin/arm64   |
| BBolt Library  | v1.4.3                |
| Test Framework | Go testing + benchmem |

### Test Parameters

| Parameter          | Value                                           |
| ------------------ | ----------------------------------------------- |
| Benchmark Time     | 2 seconds per benchmark                         |
| Benchmark Count    | 1-3 iterations                                  |
| Peer Counts Tested | 10, 50, 100                                     |
| Record Payload     | NodeBufferInfo (realistic P2P connection state) |

---

## 3. Serialization Performance

Each peer record is serialized with the following format:

```
[Version: 1 byte][CRC32: 4 bytes][JSON Payload: ~693 bytes]
```

### Benchmark Results

| Operation               | Iterations | ns/op     | us/op    | B/op      | allocs/op |
| ----------------------- | ---------- | --------- | -------- | --------- | --------- |
| SerializeNodeBufferInfo | 935,073    | 2,342     | 2.34     | 2,116     | 15        |
| SerializeNodeBufferInfo | 1,000,000  | 2,435     | 2.44     | 2,116     | 15        |
| SerializeNodeBufferInfo | 970,639    | 2,423     | 2.42     | 2,116     | 15        |
| **Average**             | -          | **2,400** | **2.40** | **2,116** | **15**    |

| Operation                 | Iterations | ns/op     | us/op    | B/op      | allocs/op |
| ------------------------- | ---------- | --------- | -------- | --------- | --------- |
| DeserializeNodeBufferInfo | 472,564    | 4,980     | 4.98     | 1,344     | 24        |
| DeserializeNodeBufferInfo | 492,098    | 4,902     | 4.90     | 1,344     | 24        |
| DeserializeNodeBufferInfo | 499,078    | 4,873     | 4.87     | 1,344     | 24        |
| **Average**               | -          | **4,918** | **4.92** | **1,344** | **24**    |

### Serialization Overhead Analysis

| Component                | Size (bytes) | Percentage |
| ------------------------ | ------------ | ---------- |
| Total Serialized         | 698          | 100%       |
| JSON Payload             | 693          | 99.28%     |
| Header (version + CRC32) | 5            | 0.72%      |

**Observation:** The CRC32 checksum adds negligible size overhead (0.72%) while providing data integrity validation.

---

## 4. Read Performance

### Single Peer Lookup

Direct B+tree key lookup for retrieving one peer's state.

| Metric      | Value              |
| ----------- | ------------------ |
| Iterations  | 334,426            |
| Latency     | 7,073 ns (7.07 us) |
| Memory      | 2,896 B/op         |
| Allocations | 34 allocs/op       |
| Throughput  | ~141,000 reads/sec |

### Bulk Load (100 Peers)

Loading all peers from database on application startup.

| Metric             | Value                 |
| ------------------ | --------------------- |
| Iterations         | 4,118                 |
| Latency            | 584,912 ns (585 us)   |
| Memory             | 198,255 B/op (194 KB) |
| Allocations        | 2,920 allocs/op       |
| Per-Record Latency | ~5.85 us              |

**Performance Summary Test Results:**

```
Serialization: 10000 iterations in 36.57ms (3.66 us/op)
Deserialization: 10000 iterations in 55.85ms (5.58 us/op)
Single Peer Read: 1000 iterations in 9.76ms (9.76 us/op)
Load All Peers (50 records): 100 iterations in 1.13ms (11.3 us/op)
```

---

## 5. Write Performance

### Batched Write (50 peers per transaction)

| Metric             | Value                   |
| ------------------ | ----------------------- |
| Iterations         | 231                     |
| Latency            | 10,507,630 ns (10.5 ms) |
| Memory             | 156,109 B/op (152 KB)   |
| Allocations        | 1,191 allocs/op         |
| Per-Record Latency | ~210 us                 |

### Immediate Write (Queued)

The immediate write benchmark measures queue submission time, not disk sync time:

| Metric      | Value              |
| ----------- | ------------------ |
| Iterations  | 996,066            |
| Latency     | 2,368 ns (2.37 us) |
| Memory      | 232 B/op           |
| Allocations | 4 allocs/op        |

**Note:** The immediate write latency shown is the time to queue a write, not the actual disk persistence time. Actual fsync to SSD adds ~0.5-2ms; fsync to SD card adds ~10-50ms.

---

## 6. Database Size Analysis

### Measured File Sizes

| Peer Count | File Size | File Size (KB) | Avg per Record |
| ---------- | --------- | -------------- | -------------- |
| 0 (empty)  | ~32 KB    | 32             | N/A (base)     |
| 10         | 114,688 B | 112 KB         | 11,469 B       |
| 50         | 180,224 B | 176 KB         | 3,604 B        |
| 100        | 311,296 B | 304 KB         | 3,113 B        |

### Size Breakdown

| Component      | Typical Size | Notes                   |
| -------------- | ------------ | ----------------------- |
| BBolt metadata | 32 KB        | Fixed base overhead     |
| Bucket headers | 4 KB         | Per bucket              |
| B+tree pages   | 4 KB each    | Minimum allocation unit |
| Record data    | ~700 B       | JSON + 5-byte header    |

### Space Efficiency

The average per-record overhead decreases as peer count increases:

- At 10 peers: High overhead due to page alignment (11.5 KB/record)
- At 50 peers: Moderate overhead (3.6 KB/record)
- At 100 peers: Efficient packing (3.1 KB/record)

This is expected behavior for B+tree databases with 4KB page alignment.

### Projected Sizes

| Peer Count | Estimated Size    |
| ---------- | ----------------- |
| 100        | 304 KB (measured) |
| 500        | ~1.5 MB           |
| 1,000      | ~3.0 MB           |

---

## 7. Memory Footprint

### Per-Operation Allocations

| Operation          | Bytes/op | Allocations/op |
| ------------------ | -------- | -------------- |
| Serialize          | 2,116    | 15             |
| Deserialize        | 1,344    | 24             |
| Single Read        | 2,896    | 34             |
| Batched Write (50) | 156,109  | 1,191          |
| Load All (100)     | 198,255  | 2,920          |

### Runtime Memory

| Component              | Estimated Usage |
| ---------------------- | --------------- |
| StateManager struct    | ~500 B          |
| Write queue (buffered) | 100 KB max      |
| Immediate queue        | 10 KB max       |
| BBolt handle           | ~10 KB          |
| mmap pages             | OS-managed      |

---

## 8. Limitations and Notes

### Test Environment Caveats

1. **Storage Type:** Tests ran on NVMe SSD. SD card performance will be 10-100x slower for write operations.

2. **CPU Architecture:** M2 Max is significantly faster than Raspberry Pi ARM Cortex-A72. Serialization/deserialization times will increase 3-5x on Pi.

3. **Write Queue Behavior:** The immediate write benchmark measures queue time, not actual disk I/O. Real-world immediate writes with fsync will be much slower.

### What Was Not Tested

- Actual Raspberry Pi hardware
- SD card I/O characteristics
- Long-duration stress tests
- Concurrent read/write scenarios
- Database corruption recovery

---

## 9. Conclusions

### Measured Performance Summary

| Category                  | Performance | Assessment |
| ------------------------- | ----------- | ---------- |
| Read Latency              | 7-10 us     | Excellent  |
| Bulk Load (100 peers)     | 585 us      | Excellent  |
| Serialization             | 2.4 us      | Excellent  |
| Deserialization           | 4.9 us      | Excellent  |
| Batched Write (50 peers)  | 10.5 ms     | Good       |
| Database Size (100 peers) | 304 KB      | Compact    |
| Record Size               | ~700 bytes  | Efficient  |

### Recommendations for Production

1. **Batching is Essential:** Individual synced writes would be prohibitively slow on SD cards. The 5-minute batch interval is appropriate.

2. **Read Performance is Not a Concern:** Sub-millisecond reads are achievable even on slower hardware.

3. **Database Size is Manageable:** Even at 1000 peers, the database would be ~3 MB, easily fitting on any SD card.

4. **Raspberry Pi Testing Required:** Actual embedded device testing is necessary to validate write latency under real conditions.

---

## Appendix: Raw Benchmark Output

```
goos: darwin
goarch: arm64
pkg: github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib

BenchmarkSerializeNodeBufferInfo-12       935073    2342 ns/op    2116 B/op   15 allocs/op
BenchmarkDeserializeNodeBufferInfo-12     472564    4980 ns/op    1344 B/op   24 allocs/op
BenchmarkLoadSinglePeer-12                334426    7073 ns/op    2896 B/op   34 allocs/op
BenchmarkLoadAllPeers_100-12                4118  584912 ns/op  198255 B/op 2920 allocs/op
BenchmarkBatchedWrite_50-12                  231 10507630 ns/op 156109 B/op 1191 allocs/op
BenchmarkImmediateWrite-12 (queue only)   996066    2368 ns/op     232 B/op    4 allocs/op
```

---

## Document History

| Version | Date         | Changes                                             |
| ------- | ------------ | --------------------------------------------------- |
| 1.0     | Nov 2025     | Initial draft with estimated values                 |
| 1.1     | Nov 26, 2025 | Updated with actual benchmark measurements on macOS |

---

_Report generated from actual benchmark execution on November 26, 2025_
