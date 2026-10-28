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

### 事前準備

1. FAT32化するSSD / USBストレージをPCへ接続します。
2. エクスプローラーで対象ドライブに `D:` や `E:` などのドライブレターが付いていることを確認します。
3. 必要なデータをすべて別の場所へバックアップします。
4. 対象ドライブを使用しているアプリやエクスプローラーのウィンドウを閉じます。

> [!CAUTION]
> このツールはexFAT / NTFS等のデータを保持したままFAT32へ変換するツールではありません。対象ボリュームを消去し、FAT32として新しく作り直します。

### 1. EXEを起動

`FAT32SSDFormatter_console_v0.2.1.exe` をダブルクリックして起動します。

管理者権限がない場合は自動的にUAC昇格を要求します。Windowsの確認画面が表示されたら許可してください。昇格に失敗する場合は、EXEを右クリックして **「管理者として実行」** を選択します。

### 2. 対象ドライブを選ぶ

起動すると、フォーマット可能な固定 / リムーバブルボリュームが表示されます。Windowsが起動しているシステムドライブは一覧から除外されます。

表示例:

```text
Available target volumes:
  1) E:  476.94 GiB  FS=exFAT     512 B/sector  MUSICSSD
  2) F:  238.47 GiB  FS=NTFS      512 B/sector  BACKUP

Select target [1-2]:
```

FAT32にするドライブの番号を入力してEnterを押します。

容量・現在のファイルシステム・ドライブレターを必ず確認してください。

### 3. ボリュームラベルを入力

```text
Volume label [FAT32SSD]:
```

そのままEnterを押すと `FAT32SSD` になります。

ラベルはFAT32互換のため最大11文字相当で、英大文字・数字・スペース・`_`・`-` が使用できます。小文字は大文字へ変換され、使用できない文字は除外されます。

### 4. クラスタサイズを選択

```text
Cluster size:  1) Auto  2) 4KB  3) 8KB  4) 16KB  5) 32KB  6) 64KB
Select [1]:
```

通常は何も入力せずEnter、または `1` を入力して **Auto** を使用してください。

大容量SSDではAutoは32 KiBを選択します。256GB / 512GB / 1TB / 2TBでは基本的にAutoを推奨します。

2TBでは4 KiBクラスタはFAT32の最大クラスタ数を超えるため使用できません。Auto（32 KiB）または8 / 16 / 32 / 64 KiBを使用してください。

### 5. 対象を最終確認

フォーマット直前に次のような内容が表示されます。

```text
WARNING: ALL DATA ON THE SELECTED VOLUME WILL BE ERASED.
Target : E:
Size   : 476.94 GiB (...)
Current: exFAT
Label  : FAT32SSD
Type exactly 'FORMAT E:' to continue:
```

表示された文字列を**完全に同じ内容で**入力します。Eドライブなら:

```text
FORMAT E:
```

続いて:

```text
Final confirmation - type YES:
```

に対して、実行する場合のみ:

```text
YES
```

と入力します。どちらかが一致しなければ処理はキャンセルされます。

### 6. フォーマット実行

確認後、ボリュームをロック・ディスマウントしてFAT32構造を書き込みます。処理中はSSDを取り外したりPCの電源を切ったりしないでください。

主な処理は以下です。

- FAT32 Boot Sector / BPB作成
- FSInfo作成
- Backup Boot Sector / Backup FSInfo作成
- FAT #1 / FAT #2初期化
- Root Directory作成
- MBRの場合は可能ならpartition typeをFAT32 LBA `0x0C`へ変更

完了すると次のように表示されます。

```text
[100%] Completed.

E: was formatted as FAT32 successfully.
```

### 7. 完了後

エクスプローラーへすぐ反映されない場合は、外付けSSD / USBストレージを安全に取り外して接続し直すか、「ディスクの管理」でディスクの再スキャンを行ってください。

Windowsのドライブのプロパティで **ファイルシステム: FAT32** になっていることを確認します。

### exFATからFAT32へ変更する場合の最短手順

```text
1. exFATのSSDを接続
2. EXEを起動してUACを許可
3. FS=exFAT と表示されている対象ドライブを選択
4. Volume labelを入力
5. Cluster sizeはAuto
6. FORMAT X: を入力
7. YES を入力
8. Completed. が出るまで待つ
9. SSDを再接続してFAT32になっていることを確認
```

**exFAT上の既存ファイルはすべて消去されます。**

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
