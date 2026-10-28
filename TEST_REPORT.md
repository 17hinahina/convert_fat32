# Test Report — FAT32 SSD Formatter Console v0.2.1

Test date: 2026-10-29 (JST)

## Result

No new functional defect was found in v0.2.1 during this test pass.

The requested exFAT → FAT32 stress test passed for all four target capacities. The 100,000-iteration test was executed twice, so each capacity completed 200,000 successful conversion checks (800,000 total).

## Target capacities

The test uses decimal manufacturer capacities, not binary TiB/GiB labels:

| Name | Bytes |
|---|---:|
| 256GB | 256,000,000,000 |
| 512GB | 512,000,000,000 |
| 1TB | 1,000,000,000,000 |
| 2TB | 2,000,000,000,000 |

This distinction matters at the upper limit: 2TB decimal is supported at 512-byte logical sectors, while 2TiB is larger than the FAT32 BPB 32-bit Total Sectors field can represent at 512 bytes/sector.

## Build / static checks

- Latest `main.go` Git blob: `ce7faf2579dd6387aee57832b28cfc2212a46cc1`
- `GOOS=windows GOARCH=amd64 go vet main.go`: PASS
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`: PASS
- Output format: PE32+ / Windows console / x86-64: PASS
- No third-party Go module dependency is required: PASS

The locally rebuilt executable SHA-256 for this pass was:

`5f54555e73c3fe1c9e116ce95b1a4067793eb8d1b29b6b1fc49c87fc4743d23b`

(Build hashes can differ across Go patch versions/toolchains even when source is identical.)

## exFAT → FAT32 stress test

Test harness: `tests/stress_exfat_to_fat32.py`

Each iteration performs the following checks:

1. Start from a checksum-valid exFAT Boot Region template.
2. Vary live exFAT state fields that are excluded from BootChecksum (`VolumeFlags` / `PercentInUse`).
3. Re-run the v0.2.1 FAT32 layout calculation for the exact target capacity.
4. Model the formatter's destructive clearing of FAT32 reserved sectors 0–31, which covers exFAT Main + Backup Boot Regions at sectors 0–23.
5. Generate the target FAT32 BPB, FSInfo, first FAT sector and root-directory metadata.
6. Validate FAT capacity, cluster/data boundaries, backup metadata equivalence, root label entry and absence of the exFAT signature from the replacement metadata.

Logical sector sizes exercised: 512 / 1024 / 2048 / 4096 bytes.

Cluster selections exercised whenever valid: Auto / 4 / 8 / 16 / 32 / 64 KiB.

### Stress results

Two deterministic runs were completed:

| Capacity | Run 1 | Run 2 | Total |
|---|---:|---:|---:|
| 256GB | 100,000 PASS | 100,000 PASS | 200,000 PASS |
| 512GB | 100,000 PASS | 100,000 PASS | 200,000 PASS |
| 1TB | 100,000 PASS | 100,000 PASS | 200,000 PASS |
| 2TB | 100,000 PASS | 100,000 PASS | 200,000 PASS |
| **Total** | **400,000 PASS** | **400,000 PASS** | **800,000 PASS** |

Deterministic result digests were identical across both runs:

- 256GB: `c5081966e39ee2046601d5e5fd9574e56e78baf3d3d28a717b4b1c53eabeb2fb`
- 512GB: `928b73280eeaafd29afa5e1587fdad30676e0a8ad2b45e97db1e00b29e265a79`
- 1TB: `0e5268fadfd5d4bc9a83e70c0adcf508a0cbb253ed75cb8c37a78b8ed63540ac`
- 2TB: `1628d5e77cf23188467ca44cfa7f6a115169fcd87225e29264ec0b0ee3007307`

### 2TB cluster-size behavior

For 2,000,000,000,000 bytes, 4 KiB clusters would create more clusters than FAT32 can address (`0x0FFFFFF5` maximum), so v0.2.1 correctly rejects that mode.

The following modes pass at 2TB for all tested logical-sector sizes:

- Auto (32 KiB)
- 8 KiB
- 16 KiB
- 32 KiB
- 64 KiB

This is expected behavior, not a defect.

## Full sparse-image exFAT → FAT32 conversion

In addition to in-memory stress iterations, one full sparse image was created for each requested capacity.

The source image contained a specification-valid exFAT Main/Backup Boot Region with correct BootChecksum. Before conversion, Linux `blkid` identified every image as `TYPE=exfat`, `VERSION=1.0`.

The FAT32 conversion then performed the same metadata-write pattern as v0.2.1:

- clear the 32 reserved sectors
- write primary BPB + FSInfo
- write backup BPB + FSInfo
- zero FAT #1
- zero FAT #2
- write FAT reserved/root-cluster entries to both FATs
- zero/create the root-directory cluster

After conversion, `blkid` identified every image as `TYPE=vfat`, `VERSION=FAT32` with the full original capacity.

| Capacity | Before | After | Auto cluster | FAT32 FAT size (each) | Result |
|---|---|---|---:|---:|---|
| 256GB | exFAT 1.0 | FAT32/vfat | 32 KiB | 31,252,480 B | PASS |
| 512GB | exFAT 1.0 | FAT32/vfat | 32 KiB | 62,513,152 B | PASS |
| 1TB | exFAT 1.0 | FAT32/vfat | 32 KiB | 122,085,376 B | PASS |
| 2TB | exFAT 1.0 | FAT32/vfat | 32 KiB | 244,146,176 B | PASS |

For each image, the test also sampled the beginning, middle and end of both FAT copies to confirm old metadata was cleared, verified the primary/backup FAT32 Boot/FSInfo structures, and verified the root-directory volume-label entry.

## Previous regression / boundary tests

The earlier v0.2.1 test pass also covered:

- 200,000 deterministic randomized FAT layout combinations
- 1 GiB / 64 GiB FAT32 image recognition
- 1,896,602,889,216-byte v0.2.0 regression capacity
- 2,000,000,000,000-byte images at 512- and 4096-byte logical sectors
- FAT entry capacity and data-boundary invariants

All passed.

## Windows raw-device integration boundary

The stress and sparse-image tests validate filesystem layout and exFAT-metadata replacement, but this test host is Linux. It cannot execute the Windows-only device-control path itself:

- UAC elevation
- `CreateFileW("\\\\.\\X:")`
- `FSCTL_LOCK_VOLUME`
- `FSCTL_DISMOUNT_VOLUME`
- raw `WriteFile` through a Windows volume handle
- `IOCTL_DISK_SET_PARTITION_INFO` on MBR media
- Windows remount / Explorer refresh behavior

Therefore, the 200,000-per-capacity count is a filesystem-conversion stress test, **not 200,000 physical SSD erase/write cycles**. Performing 100,000 full 2TB-device rewrites would be impractical and would impose extreme write wear. A final destructive integration check should still use an empty disposable USB device or VHD/VHDX on Windows.
