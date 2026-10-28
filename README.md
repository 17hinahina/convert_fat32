# convert_fat32

Windows 10 / 11 x64向けのコンソール型FAT32フォーマッタです。Windows標準GUIの大容量FAT32フォーマット制限に依存せず、既存のドライブレター付きボリュームを最大約2TBまでFAT32として初期化することを目的としています。

> [!WARNING]
> フォーマット対象のデータはすべて消去されます。対象ドライブを必ず確認し、必要なデータは事前にバックアップしてください。

## Current version

`v0.2.1`

v0.2.0で特定容量に発生していたFATサイズ計算の振動問題を修正しています。

## Features

- Windows 10 / 11 x64
- 管理者権限で動作
- 最大約2TBのFAT32を想定
- 512 / 1024 / 2048 / 4096-byte logical sector対応
- Auto / 4 / 8 / 16 / 32 / 64 KiB cluster選択
- BPB / FSInfo / FATを直接生成
- FATを2コピー作成
- バックアップBoot Sector / FSInfoを生成
- フォーマット前にボリュームを排他ロック
- Windowsが起動しているドライブは候補から除外
- MBRの場合はFAT32 LBA partition type `0x0C` への変更を試行
- 外部Goライブラリ不要

## Usage

1. FAT32化するSSD / USBストレージにドライブレターを割り当てます。
2. `FAT32SSDFormatter_console_v0.2.1.exe` を実行します。
3. UACを許可します。
4. 一覧から対象ドライブを選択します。
5. ボリュームラベルを入力します。
6. クラスタサイズを選択します。通常は `Auto` で構いません。
7. 表示された `FORMAT X:` を正確に入力します。
8. 最終確認として `YES` を入力するとフォーマットを開始します。

大容量ドライブではAuto選択時に32 KiBクラスタを使用します。

## Important behavior

このプログラムは**物理ディスク全体を再パーティションするツールではありません**。すでに存在してドライブレターが付いているボリュームをFAT32化します。

2TB SSDを1パーティションで使用したい場合は、必要に応じて事前にWindowsの「ディスクの管理」等でパーティションを作成してください。古い機器ではGPTではなくMBRが要求される場合があります。

FAT32には1ファイル約4GBのサイズ上限があります。また、FAT32として正しく作成できても、接続先のDJ機器・オーディオ機器・組み込み機器側に容量、パーティション方式、クラスタサイズ等の追加制限がある場合があります。

## Build

Goがインストールされた環境からWindows x64向けにビルドできます。

```powershell
$env:GOOS="windows"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
go build -trimpath -ldflags="-s -w" -o FAT32SSDFormatter_console_v0.2.1.exe main.go
```

Linux / macOSからのクロスビルド例:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags='-s -w' \
  -o FAT32SSDFormatter_console_v0.2.1.exe main.go
```

## Test status

v0.2.1では以下を確認しています。

- Windows amd64向け `go vet`: PASS
- Windows amd64向けクロスビルド: PASS
- 200,000パターンのFATレイアウト計算: PASS
- 1 GiB / 64 GiB / 約1.896 TB / 2 TBのFAT32イメージ認識: PASS
- 2 TB・4096-byte logical sectorのイメージ認識: PASS
- v0.2.0で失敗していた約1.896 TBケースの回帰テスト: PASS
- exFAT → FAT32変換ストレス: **256GB / 512GB / 1TB / 2TBを各200,000回 PASS**（100,000回×2実行、合計800,000回）
- 上記4容量のスパースイメージを `blkid` で変換前 `exFAT`、変換後 `FAT32/vfat` と認識: PASS

ストレステストでは512 / 1024 / 2048 / 4096-byte logical sectorと、各容量で成立するAuto / 4 / 8 / 16 / 32 / 64 KiB clusterモードを組み合わせています。2TBでは4 KiB clusterはFAT32の最大クラスタ数を超えるため、プログラム仕様どおり拒否対象です。Auto（32 KiB）および8 / 16 / 32 / 64 KiBはPASSしています。

詳細は [`TEST_REPORT.md`](TEST_REPORT.md) を参照してください。再実行用テストは [`tests/stress_exfat_to_fat32.py`](tests/stress_exfat_to_fat32.py) にあります。

Windows実機でのUAC・ボリュームロック・ディスマウント・raw writeを伴う破壊的統合テストは、このLinuxテスト環境では実行していません。最初は空のUSBメモリやVHD/VHDX等で確認してください。

## FAT32 capacity note

FAT32 BPBのTotal Sectorsフィールドは32-bitです。512-byte logical sectorの場合、表現できる理論上限は `2,199,023,255,040 bytes`（2 TiB - 512 bytes）です。

一般に「2TB」として販売される `2,000,000,000,000 bytes` 前後のSSDはこの範囲内です。

## Files

- `main.go` — formatter source
- `TEST_REPORT.md` — v0.2.1 test report
- `tests/stress_exfat_to_fat32.py` — exFAT → FAT32 stress/regression test
- `.github/workflows/build.yml` — Windows x64 build + stress-test workflow

## Disclaimer

本ツールはストレージへ直接書き込みを行います。使用前に対象ドライブを十分確認してください。本ツールの使用によるデータ消失や接続先機器との非互換については利用者自身で確認してください。
