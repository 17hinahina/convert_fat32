# Test Report — FAT32 SSD Formatter Console v0.2.1

Test date: 2026-10-29 (JST)

## Result

No new functional defect was found in v0.2.1 during this test pass.

## Build / static checks

- `GOOS=windows GOARCH=amd64 go vet main.go`: PASS
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`: PASS
- Output format: PE32+ / Windows console / x86-64: PASS
- No third-party Go module dependency is required: PASS

The source was rebuilt after the v0.2.1 header comment correction. The resulting tested executable SHA-256 is:

`4ae4907f3862630c44c2644143e5459230cb69feb676b20f6b60976b897a42fb`

## FAT32 layout regression / boundary tests

The FAT sizing and layout calculations were exercised with 200,000 deterministic randomized combinations covering:

- Logical sector sizes: 512 / 1024 / 2048 / 4096 bytes
- Cluster selections: Auto / 4 KiB / 8 KiB / 16 KiB / 32 KiB / 64 KiB
- Volume sizes from approximately 33 MiB through 2,000,000,000,000 bytes

Validated invariants included:

- FAT entry capacity is large enough for every data cluster plus reserved entries
- FAT/data boundary alignment is valid
- The data region never extends past the reported volume size
- Remaining tail sectors are smaller than one cluster
- FAT32 cluster-count range checks are enforced
- The v0.2.0 FAT-size oscillation regression does not recur

Result: PASS (200,000 / 200,000 layouts)

## Filesystem image recognition tests

Sparse test images were generated using the same BPB / FSInfo / FAT layout rules and probed with Linux `blkid`.

| Test image | Logical sector | Auto cluster | Result |
|---|---:|---:|---|
| 1 GiB | 512 B | 4 KiB | FAT32/vfat recognized |
| 64 GiB | 512 B | 32 KiB | FAT32/vfat recognized |
| 1,896,602,889,216 B regression case | 512 B | 32 KiB | FAT32/vfat recognized |
| 2,000,000,000,000 B | 512 B | 32 KiB | FAT32/vfat recognized |
| 2,000,000,000,000 B | 4096 B | 32 KiB | FAT32/vfat recognized |

Result: PASS

## 2 TB boundary

- 2 TB decimal (`2,000,000,000,000` bytes): supported with 512-byte logical sectors.
- FAT32 BPB uses a 32-bit total-sector field.
- At 512-byte logical sectors the largest representable volume using this field is `0xFFFFFFFF × 512 = 2,199,023,255,040` bytes (2 TiB minus 512 bytes).
- Therefore ordinary marketed 2 TB SSDs are below the BPB total-sector limit.

## Regression fixed in v0.2.1

v0.2.0 could oscillate between adjacent FAT sizes for certain capacities and fail before formatting with `could not converge FAT size`.

v0.2.1 grows the FAT monotonically and accepts it once its entry capacity is sufficient. The previously failing 1,896,602,889,216-byte case now passes the layout and filesystem-recognition tests.

## Windows raw-device integration boundary

This test host is Linux, so the actual Windows-only path cannot be executed here:

- UAC elevation
- `CreateFileW("\\\\.\\X:")`
- `FSCTL_LOCK_VOLUME`
- `FSCTL_DISMOUNT_VOLUME`
- raw `WriteFile` to the selected Windows volume
- `IOCTL_DISK_SET_PARTITION_INFO` on MBR media
- remount / Explorer refresh behavior

The Win32 calls used for volume locking follow Microsoft's documented direct-volume-handle pattern. A final destructive integration test should still be performed on Windows with an empty disposable USB drive or VHD/VHDX before using the program on valuable media.
