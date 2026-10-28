// FAT32 SSD Formatter Console v0.2.1
// Windows x64 console utility. Destructive: formats an existing mounted volume as FAT32.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	version = "0.2.1"

	genericRead  = 0x80000000
	genericWrite = 0x40000000
	shareRead    = 0x00000001
	shareWrite   = 0x00000002
	openExisting = 3

	driveRemovable = 2
	driveFixed     = 3

	ioctlDiskGetLengthInfo      = 0x0007405C
	ioctlDiskGetPartitionInfoEx = 0x00070048
	ioctlDiskSetPartitionInfo   = 0x0007C008
	ioctlStorageQueryProperty   = 0x002D1400
	fsctlLockVolume             = 0x00090018
	fsctlUnlockVolume           = 0x0009001C
	fsctlDismountVolume         = 0x00090020

	reservedSectors  = uint32(32)
	numberOfFATs     = uint32(2)
	rootCluster      = uint32(2)
	minFAT32Clusters = uint64(65525)
	maxFAT32Clusters = uint64(0x0FFFFFF5)
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procCreateFileW           = kernel32.NewProc("CreateFileW")
	procCloseHandle           = kernel32.NewProc("CloseHandle")
	procDeviceIoControl       = kernel32.NewProc("DeviceIoControl")
	procSetFilePointerEx      = kernel32.NewProc("SetFilePointerEx")
	procWriteFile             = kernel32.NewProc("WriteFile")
	procFlushFileBuffers      = kernel32.NewProc("FlushFileBuffers")
	procGetLogicalDrives      = kernel32.NewProc("GetLogicalDrives")
	procGetDriveTypeW         = kernel32.NewProc("GetDriveTypeW")
	procGetWindowsDirectoryW  = kernel32.NewProc("GetWindowsDirectoryW")
	procGetDiskFreeSpaceExW   = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetDiskFreeSpaceW     = kernel32.NewProc("GetDiskFreeSpaceW")
	procGetVolumeInformationW = kernel32.NewProc("GetVolumeInformationW")

	procIsUserAnAdmin = shell32.NewProc("IsUserAnAdmin")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

type handle uintptr

const invalidHandle = ^handle(0)

type volumeInfo struct {
	letter         byte
	totalBytes     uint64
	bytesPerSector uint32
	fsName         string
	label          string
}

type formatOptions struct {
	letter       byte
	volumeLabel  string
	clusterBytes uint32 // 0 = auto
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func winErr(prefix string, e error) error {
	if e == nil || e == syscall.Errno(0) {
		return errors.New(prefix)
	}
	return fmt.Errorf("%s: %v", prefix, e)
}

func createFile(path string, access uint32) (handle, error) {
	r, _, e := procCreateFileW.Call(
		uintptr(unsafe.Pointer(utf16Ptr(path))),
		uintptr(access),
		uintptr(shareRead|shareWrite),
		0,
		uintptr(openExisting),
		0,
		0,
	)
	h := handle(r)
	if h == invalidHandle {
		return h, winErr("CreateFileW failed", e)
	}
	return h, nil
}

func closeHandle(h handle) {
	if h != invalidHandle && h != 0 {
		procCloseHandle.Call(uintptr(h))
	}
}

func deviceIoControl(h handle, code uint32, in []byte, out []byte) error {
	var inPtr, outPtr uintptr
	if len(in) > 0 {
		inPtr = uintptr(unsafe.Pointer(&in[0]))
	}
	if len(out) > 0 {
		outPtr = uintptr(unsafe.Pointer(&out[0]))
	}
	var returned uint32
	r, _, e := procDeviceIoControl.Call(
		uintptr(h), uintptr(code),
		inPtr, uintptr(len(in)),
		outPtr, uintptr(len(out)),
		uintptr(unsafe.Pointer(&returned)), 0,
	)
	if r == 0 {
		return winErr(fmt.Sprintf("DeviceIoControl(0x%08X) failed", code), e)
	}
	return nil
}

func setFilePointer(h handle, offset uint64) error {
	off := int64(offset)
	r, _, e := procSetFilePointerEx.Call(uintptr(h), uintptr(off), 0, 0)
	if r == 0 {
		return winErr("SetFilePointerEx failed", e)
	}
	return nil
}

func writeAll(h handle, offset uint64, data []byte) error {
	if err := setFilePointer(h, offset); err != nil {
		return err
	}
	done := 0
	for done < len(data) {
		var written uint32
		r, _, e := procWriteFile.Call(
			uintptr(h),
			uintptr(unsafe.Pointer(&data[done])),
			uintptr(len(data)-done),
			uintptr(unsafe.Pointer(&written)),
			0,
		)
		if r == 0 || written == 0 {
			return winErr("WriteFile failed", e)
		}
		done += int(written)
	}
	return nil
}

func flush(h handle) error {
	r, _, e := procFlushFileBuffers.Call(uintptr(h))
	if r == 0 {
		return winErr("FlushFileBuffers failed", e)
	}
	return nil
}

func volumePath(letter byte) string { return fmt.Sprintf(`\\.\%c:`, letter) }
func rootPath(letter byte) string   { return fmt.Sprintf(`%c:\`, letter) }

func getWindowsDrive() byte {
	buf := make([]uint16, 32768)
	r, _, _ := procGetWindowsDirectoryW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r > 1 {
		s := syscall.UTF16ToString(buf)
		if len(s) >= 2 && s[1] == ':' {
			c := s[0]
			if c >= 'a' && c <= 'z' {
				c -= 32
			}
			return c
		}
	}
	return 'C'
}

func isAdmin() bool {
	r, _, _ := procIsUserAnAdmin.Call()
	return r != 0
}

func relaunchElevated() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	exe, _ = filepath.Abs(exe)
	verb := utf16Ptr("runas")
	file := utf16Ptr(exe)
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 1)
	return r > 32
}

func driveType(root string) uint32 {
	r, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(utf16Ptr(root))))
	return uint32(r)
}

func getDiskFreeSpaceEx(root string) uint64 {
	var avail, total, free uint64
	r, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(utf16Ptr(root))),
		uintptr(unsafe.Pointer(&avail)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)),
	)
	if r == 0 {
		return 0
	}
	return total
}

func getVolumeStrings(root string) (label, fs string) {
	labelBuf := make([]uint16, 256)
	fsBuf := make([]uint16, 64)
	var serial, maxComp, flags uint32
	r, _, _ := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(utf16Ptr(root))),
		uintptr(unsafe.Pointer(&labelBuf[0])), uintptr(len(labelBuf)),
		uintptr(unsafe.Pointer(&serial)), uintptr(unsafe.Pointer(&maxComp)), uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&fsBuf[0])), uintptr(len(fsBuf)),
	)
	if r == 0 {
		return "", "Unknown"
	}
	return syscall.UTF16ToString(labelBuf), syscall.UTF16ToString(fsBuf)
}

func queryVolume(letter byte) (volumeInfo, error) {
	vi := volumeInfo{letter: letter}
	h, err := createFile(volumePath(letter), genericRead)
	if err != nil {
		return vi, err
	}
	defer closeHandle(h)

	outLen := make([]byte, 8)
	if err := deviceIoControl(h, ioctlDiskGetLengthInfo, nil, outLen); err != nil {
		return vi, err
	}
	vi.totalBytes = binary.LittleEndian.Uint64(outLen)

	// STORAGE_PROPERTY_QUERY: PropertyId=StorageAccessAlignmentProperty(6), QueryType=PropertyStandardQuery(0)
	query := make([]byte, 12)
	binary.LittleEndian.PutUint32(query[0:4], 6)
	align := make([]byte, 32)
	if err := deviceIoControl(h, ioctlStorageQueryProperty, query, align); err == nil {
		vi.bytesPerSector = binary.LittleEndian.Uint32(align[16:20])
	}

	if vi.bytesPerSector == 0 {
		var spc, bps, freeClusters, totalClusters uint32
		r, _, _ := procGetDiskFreeSpaceW.Call(
			uintptr(unsafe.Pointer(utf16Ptr(rootPath(letter)))),
			uintptr(unsafe.Pointer(&spc)), uintptr(unsafe.Pointer(&bps)),
			uintptr(unsafe.Pointer(&freeClusters)), uintptr(unsafe.Pointer(&totalClusters)),
		)
		if r != 0 {
			vi.bytesPerSector = bps
		}
	}
	if vi.bytesPerSector == 0 {
		return vi, errors.New("could not determine logical sector size")
	}

	vi.label, vi.fsName = getVolumeStrings(rootPath(letter))
	return vi, nil
}

func listVolumes() []volumeInfo {
	mask, _, _ := procGetLogicalDrives.Call()
	sysDrive := getWindowsDrive()
	vols := make([]volumeInfo, 0)
	for i := 0; i < 26; i++ {
		if mask&(uintptr(1)<<uintptr(i)) == 0 {
			continue
		}
		letter := byte('A' + i)
		if letter == sysDrive {
			continue
		}
		dt := driveType(rootPath(letter))
		if dt != driveFixed && dt != driveRemovable {
			continue
		}
		vi, err := queryVolume(letter)
		if err != nil {
			total := getDiskFreeSpaceEx(rootPath(letter))
			if total == 0 {
				continue
			}
			vi = volumeInfo{letter: letter, totalBytes: total, fsName: "Unknown"}
		}
		vols = append(vols, vi)
	}
	return vols
}

func humanBytes(v uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	f := float64(v)
	u := 0
	for f >= 1024 && u < len(units)-1 {
		f /= 1024
		u++
	}
	if u >= 3 {
		return fmt.Sprintf("%.2f %s", f, units[u])
	}
	return fmt.Sprintf("%.1f %s", f, units[u])
}

func isPowerOfTwo(v uint32) bool { return v != 0 && (v&(v-1)) == 0 }

func autoClusterBytes(bytes uint64, bps uint32) uint32 {
	c := uint32(32 * 1024)
	gib := uint64(1024 * 1024 * 1024)
	if bytes <= 8*gib {
		c = 4 * 1024
	} else if bytes <= 16*gib {
		c = 8 * 1024
	} else if bytes <= 32*gib {
		c = 16 * 1024
	}
	if c < bps {
		c = bps
	}
	return c
}

func computeFatSectors(totalSectors uint32, bps uint32, spc uint32) (uint32, uint64, error) {
	// Grow the FAT monotonically until it is large enough for the cluster count.
	// Requiring exact equality can oscillate between N and N+1 sectors for some
	// volume sizes because changing the FAT size also changes the data-cluster count.
	fatSectors := uint32(1)
	for i := 0; i < 64; i++ {
		used := uint64(reservedSectors) + uint64(numberOfFATs)*uint64(fatSectors)
		if used >= uint64(totalSectors) {
			return 0, 0, errors.New("volume is too small")
		}
		dataSectors := uint64(totalSectors) - used
		clusters := dataSectors / uint64(spc)
		fatBytes := (clusters + 2) * 4
		required := uint32((fatBytes + uint64(bps) - 1) / uint64(bps))
		if required <= fatSectors {
			for (reservedSectors+numberOfFATs*fatSectors)%spc != 0 {
				fatSectors++
			}
			alignedUsed := uint64(reservedSectors) + uint64(numberOfFATs)*uint64(fatSectors)
			if alignedUsed >= uint64(totalSectors) {
				return 0, 0, errors.New("invalid FAT layout")
			}
			clusterCount := (uint64(totalSectors) - alignedUsed) / uint64(spc)
			capacityEntries := uint64(fatSectors) * uint64(bps) / 4
			if capacityEntries < clusterCount+2 {
				return 0, 0, errors.New("FAT is too small for computed cluster count")
			}
			return fatSectors, clusterCount, nil
		}
		fatSectors = required
	}
	return 0, 0, errors.New("could not converge FAT size")
}

func sanitizeLabel(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 11 {
			break
		}
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func label11(label string) [11]byte {
	var out [11]byte
	for i := range out {
		out[i] = ' '
	}
	label = sanitizeLabel(label)
	if label == "" {
		label = "NO NAME"
	}
	copy(out[:], []byte(label))
	return out
}

func put16(b []byte, off int, v uint16) { binary.LittleEndian.PutUint16(b[off:off+2], v) }
func put32(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:off+4], v) }

func zeroRange(h handle, offset, bytes uint64, startPct, endPct int, stage string) error {
	const chunkSize = 8 * 1024 * 1024
	zeros := make([]byte, chunkSize)
	var done uint64
	lastPct := -1
	for done < bytes {
		n := uint64(chunkSize)
		if bytes-done < n {
			n = bytes - done
		}
		if err := writeAll(h, offset+done, zeros[:int(n)]); err != nil {
			return err
		}
		done += n
		pct := startPct
		if bytes > 0 {
			pct += int(uint64(endPct-startPct) * done / bytes)
		}
		if pct != lastPct {
			fmt.Printf("\r[%3d%%] %-32s", pct, stage)
			lastPct = pct
		}
	}
	return nil
}

func formatFAT32(opt formatOptions) error {
	if opt.letter < 'A' || opt.letter > 'Z' {
		return errors.New("invalid drive letter")
	}
	if opt.letter == getWindowsDrive() {
		return errors.New("refusing to format the Windows system drive")
	}

	h, err := createFile(volumePath(opt.letter), genericRead|genericWrite)
	if err != nil {
		return fmt.Errorf("cannot open target volume for writing (run as Administrator): %w", err)
	}
	defer closeHandle(h)

	if err := deviceIoControl(h, fsctlLockVolume, nil, nil); err != nil {
		return fmt.Errorf("cannot lock volume; close Explorer windows and applications using %c:: %w", opt.letter, err)
	}
	locked := true
	defer func() {
		if locked {
			_ = deviceIoControl(h, fsctlUnlockVolume, nil, nil)
		}
	}()

	fmt.Printf("[  1%%] Reading volume geometry...\n")
	lenBuf := make([]byte, 8)
	if err := deviceIoControl(h, ioctlDiskGetLengthInfo, nil, lenBuf); err != nil {
		return err
	}
	volumeBytes := binary.LittleEndian.Uint64(lenBuf)

	query := make([]byte, 12)
	binary.LittleEndian.PutUint32(query[0:4], 6)
	align := make([]byte, 32)
	var bps uint32
	if err := deviceIoControl(h, ioctlStorageQueryProperty, query, align); err == nil {
		bps = binary.LittleEndian.Uint32(align[16:20])
	}
	if bps == 0 {
		var spc0, bps0, freeClusters, totalClusters uint32
		r, _, _ := procGetDiskFreeSpaceW.Call(
			uintptr(unsafe.Pointer(utf16Ptr(rootPath(opt.letter)))),
			uintptr(unsafe.Pointer(&spc0)), uintptr(unsafe.Pointer(&bps0)),
			uintptr(unsafe.Pointer(&freeClusters)), uintptr(unsafe.Pointer(&totalClusters)),
		)
		if r != 0 {
			bps = bps0
		}
	}
	if !isPowerOfTwo(bps) || bps < 512 || bps > 4096 {
		return fmt.Errorf("unsupported logical sector size: %d (supported 512/1024/2048/4096)", bps)
	}

	totalSectors64 := volumeBytes / uint64(bps)
	if totalSectors64 == 0 || totalSectors64 > 0xFFFFFFFF {
		return fmt.Errorf("volume is too large for FAT32 BPB at %d-byte sectors (%d sectors)", bps, totalSectors64)
	}
	totalSectors := uint32(totalSectors64)

	// PARTITION_INFORMATION_EX starts with PARTITION_STYLE (DWORD), then 4 bytes padding,
	// then StartingOffset (LARGE_INTEGER). This gives BPB_HiddSec for both MBR and GPT.
	hidden := uint32(0)
	partitionStyle := uint32(2) // PARTITION_STYLE_RAW fallback
	partInfoEx := make([]byte, 144)
	if err := deviceIoControl(h, ioctlDiskGetPartitionInfoEx, nil, partInfoEx); err == nil {
		partitionStyle = binary.LittleEndian.Uint32(partInfoEx[0:4])
		startingOffset := binary.LittleEndian.Uint64(partInfoEx[8:16])
		hidden64 := startingOffset / uint64(bps)
		if hidden64 <= 0xFFFFFFFF {
			hidden = uint32(hidden64)
		}
	}

	clusterBytes := opt.clusterBytes
	if clusterBytes == 0 {
		clusterBytes = autoClusterBytes(volumeBytes, bps)
	}
	if !isPowerOfTwo(clusterBytes) || clusterBytes < bps || clusterBytes > 64*1024 || clusterBytes%bps != 0 {
		return fmt.Errorf("invalid cluster size: %d bytes", clusterBytes)
	}
	spc := clusterBytes / bps
	if !isPowerOfTwo(spc) || spc == 0 || spc > 128 {
		return errors.New("invalid sectors-per-cluster")
	}

	fatSectors, clusterCount, err := computeFatSectors(totalSectors, bps, spc)
	if err != nil {
		return err
	}
	if clusterCount < minFAT32Clusters {
		return errors.New("volume is too small for FAT32 with the selected cluster size")
	}
	if clusterCount > maxFAT32Clusters {
		return errors.New("too many clusters for FAT32; select a larger cluster size")
	}

	boot := make([]byte, bps)
	boot[0], boot[1], boot[2] = 0xEB, 0x58, 0x90
	copy(boot[3:11], []byte("MSWIN4.1"))
	put16(boot, 11, uint16(bps))
	boot[13] = byte(spc)
	put16(boot, 14, uint16(reservedSectors))
	boot[16] = byte(numberOfFATs)
	put16(boot, 17, 0)
	put16(boot, 19, 0)
	boot[21] = 0xF8
	put16(boot, 22, 0)
	put16(boot, 24, 63)
	put16(boot, 26, 255)
	put32(boot, 28, hidden)
	put32(boot, 32, totalSectors)
	put32(boot, 36, fatSectors)
	put16(boot, 40, 0)
	put16(boot, 42, 0)
	put32(boot, 44, rootCluster)
	put16(boot, 48, 1)
	put16(boot, 50, 6)
	boot[64] = 0x80
	boot[66] = 0x29
	volumeID := uint32(time.Now().UnixNano()) ^ uint32(time.Now().Unix())
	put32(boot, 67, volumeID)
	l11 := label11(opt.volumeLabel)
	copy(boot[71:82], l11[:])
	copy(boot[82:90], []byte("FAT32   "))
	copy(boot[90:], []byte("FAT32 data volume"))
	boot[510], boot[511] = 0x55, 0xAA

	fsinfo := make([]byte, bps)
	put32(fsinfo, 0, 0x41615252)
	put32(fsinfo, 484, 0x61417272)
	put32(fsinfo, 488, uint32(clusterCount-1))
	put32(fsinfo, 492, 3)
	put32(fsinfo, 508, 0xAA550000)

	fatBytes := uint64(fatSectors) * uint64(bps)
	fat1Offset := uint64(reservedSectors) * uint64(bps)
	fat2Offset := fat1Offset + fatBytes
	dataOffset := fat2Offset + fatBytes
	rootOffset := dataOffset
	rootBytes := uint64(spc) * uint64(bps)

	// Dismount only after all non-destructive queries have completed. From this point onward,
	// writes go to the raw locked volume and the old filesystem cannot flush over them.
	if err := deviceIoControl(h, fsctlDismountVolume, nil, nil); err != nil {
		return fmt.Errorf("cannot dismount locked volume: %w", err)
	}

	if err := zeroRange(h, 0, uint64(reservedSectors)*uint64(bps), 3, 6, "Clearing reserved area..."); err != nil {
		return err
	}
	fmt.Println()
	if err := writeAll(h, 0, boot); err != nil {
		return err
	}
	if err := writeAll(h, uint64(1)*uint64(bps), fsinfo); err != nil {
		return err
	}
	if err := writeAll(h, uint64(6)*uint64(bps), boot); err != nil {
		return err
	}
	if err := writeAll(h, uint64(7)*uint64(bps), fsinfo); err != nil {
		return err
	}

	if err := zeroRange(h, fat1Offset, fatBytes, 8, 48, "Initializing FAT 1..."); err != nil {
		return err
	}
	fmt.Println()
	if err := zeroRange(h, fat2Offset, fatBytes, 48, 88, "Initializing FAT 2..."); err != nil {
		return err
	}
	fmt.Println()

	firstFATSector := make([]byte, bps)
	put32(firstFATSector, 0, 0x0FFFFFF8)
	put32(firstFATSector, 4, 0x0FFFFFFF)
	put32(firstFATSector, 8, 0x0FFFFFFF)
	if err := writeAll(h, fat1Offset, firstFATSector); err != nil {
		return err
	}
	if err := writeAll(h, fat2Offset, firstFATSector); err != nil {
		return err
	}

	if err := zeroRange(h, rootOffset, rootBytes, 91, 94, "Creating root directory..."); err != nil {
		return err
	}
	fmt.Println()
	cleanedLabel := sanitizeLabel(opt.volumeLabel)
	if cleanedLabel != "" {
		dirSector := make([]byte, bps)
		copy(dirSector[:11], l11[:])
		dirSector[11] = 0x08
		if err := writeAll(h, rootOffset, dirSector); err != nil {
			return err
		}
	}

	if err := flush(h); err != nil {
		return err
	}

	// On MBR media, change the partition ID from (for example) NTFS/exFAT 0x07
	// to FAT32 LBA 0x0C. GPT has no filesystem-specific partition ID to change here.
	if partitionStyle == 0 { // PARTITION_STYLE_MBR
		_ = deviceIoControl(h, ioctlDiskSetPartitionInfo, []byte{0x0C}, nil) // best effort
	}

	fmt.Printf("[ 97%%] Releasing volume...\n")
	_ = deviceIoControl(h, fsctlUnlockVolume, nil, nil)
	locked = false
	fmt.Printf("[100%%] Completed.\n")
	return nil
}

func readLine(sc *bufio.Scanner, prompt string) string {
	fmt.Print(prompt)
	if !sc.Scan() {
		return ""
	}
	return strings.TrimSpace(sc.Text())
}

func main() {
	fmt.Printf("FAT32 SSD Formatter Console v%s\n", version)
	fmt.Println("Windows x64 / FAT32 formatter for existing drive-letter volumes")
	fmt.Println()

	if !isAdmin() {
		fmt.Println("Administrator privileges are required. Requesting elevation...")
		if relaunchElevated() {
			return
		}
		fmt.Println("Elevation was cancelled or failed. Right-click the EXE and choose 'Run as administrator'.")
		fmt.Print("Press Enter to exit...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		return
	}

	vols := listVolumes()
	if len(vols) == 0 {
		fmt.Println("No eligible fixed/removable volumes were found (the Windows system drive is hidden).")
		fmt.Print("Press Enter to exit...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		return
	}

	fmt.Println("Available target volumes:")
	for i, v := range vols {
		bps := "?"
		if v.bytesPerSector != 0 {
			bps = strconv.Itoa(int(v.bytesPerSector)) + " B/sector"
		}
		label := v.label
		if label == "" {
			label = "(no label)"
		}
		fmt.Printf("  %d) %c:  %-10s  FS=%-8s  %-12s  %s\n", i+1, v.letter, humanBytes(v.totalBytes), v.fsName, bps, label)
	}
	fmt.Println()

	sc := bufio.NewScanner(os.Stdin)
	choice := readLine(sc, fmt.Sprintf("Select target [1-%d]: ", len(vols)))
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(vols) {
		fmt.Println("Invalid selection.")
		return
	}
	target := vols[n-1]

	label := readLine(sc, "Volume label [FAT32SSD]: ")
	if label == "" {
		label = "FAT32SSD"
	}
	label = sanitizeLabel(label)

	fmt.Println("Cluster size:  1) Auto  2) 4KB  3) 8KB  4) 16KB  5) 32KB  6) 64KB")
	csel := readLine(sc, "Select [1]: ")
	var cluster uint32
	switch csel {
	case "", "1":
		cluster = 0
	case "2":
		cluster = 4 * 1024
	case "3":
		cluster = 8 * 1024
	case "4":
		cluster = 16 * 1024
	case "5":
		cluster = 32 * 1024
	case "6":
		cluster = 64 * 1024
	default:
		fmt.Println("Invalid cluster-size selection.")
		return
	}

	fmt.Println()
	fmt.Println("WARNING: ALL DATA ON THE SELECTED VOLUME WILL BE ERASED.")
	fmt.Printf("Target : %c:\n", target.letter)
	fmt.Printf("Size   : %s (%d bytes)\n", humanBytes(target.totalBytes), target.totalBytes)
	fmt.Printf("Current: %s\n", target.fsName)
	fmt.Printf("Label  : %s\n", label)
	phrase := fmt.Sprintf("FORMAT %c:", target.letter)
	confirm := readLine(sc, fmt.Sprintf("Type exactly '%s' to continue: ", phrase))
	if confirm != phrase {
		fmt.Println("Cancelled.")
		return
	}
	confirm2 := readLine(sc, "Final confirmation - type YES: ")
	if confirm2 != "YES" {
		fmt.Println("Cancelled.")
		return
	}

	fmt.Println()
	err = formatFAT32(formatOptions{letter: target.letter, volumeLabel: label, clusterBytes: cluster})
	if err != nil {
		fmt.Printf("\nFORMAT FAILED: %v\n", err)
		fmt.Print("Press Enter to exit...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(1)
	}
	fmt.Printf("\n%c: was formatted as FAT32 successfully.\n", target.letter)
	fmt.Println("If Explorer does not refresh immediately, unplug/replug the external SSD or rescan disks.")
	fmt.Print("Press Enter to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}
