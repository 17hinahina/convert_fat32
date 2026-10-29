# convert_fat32

Windows 10 / 11 x64向けのコンソール型FAT32フォーマッタです。

Windows標準GUIでは大容量ドライブをFAT32としてフォーマットできませんが、このツールは既存のドライブレター付きボリュームへFAT32構造を直接作成し、最大約2TBまでのSSD / USBストレージをFAT32として初期化できます。

> [!WARNING]
> **フォーマット対象のデータはすべて消去されます。**
> 対象ドライブを必ず確認し、必要なデータは事前にバックアップしてください。

## Current version

`v0.2.4`

実行ファイル:

`FAT32SSDFormatter_console_v0.2.4.exe`

## Features

- Windows 10 / 11 x64対応
- コンソールアプリケーション
- 管理者権限で動作
- 最大約2TBのFAT32ボリュームを想定
- 512 / 1024 / 2048 / 4096-byte logical sector対応
- Auto / 4 / 8 / 16 / 32 / 64 KiB cluster選択
- FAT32 Boot Sector / BPBを直接生成
- FSInfoを生成
- FATを2コピー作成
- Backup Boot Sector / Backup FSInfoを生成
- Root Directoryを初期化
- フォーマット対象ボリュームを排他ロック
- Windowsが起動しているシステムドライブを候補から除外
- MBRの場合はFAT32 LBA partition type `0x0C` への変更を試行
- RAW I/O用バッファを64KB境界へアライン
- `FILE_FLAG_NO_BUFFERING` / `FILE_FLAG_WRITE_THROUGH` を使用
- Extended DASD I/Oを有効化してFAT32メタデータを直接書き込み
- 外部Goライブラリ不要

## Usage

### 1. SSD / USBストレージを接続

FAT32化したいSSDやUSBストレージをPCへ接続してください。

対象ドライブには `D:` や `E:` のようなドライブレターが付いている必要があります。

必要なデータはすべて別の場所へバックアップし、対象ドライブを使用しているアプリやエクスプローラーのウィンドウを閉じてください。

> [!CAUTION]
> このツールはexFAT / NTFSなどのデータを保持したままFAT32へ変換するツールではありません。
> 対象ボリュームを消去し、FAT32として新しく作り直します。

### 2. EXEを起動

`FAT32SSDFormatter_console_v0.2.4.exe` をダブルクリックして起動します。

管理者権限がない場合はUAC昇格を要求します。Windowsの確認画面が表示されたら許可してください。

昇格に失敗する場合はEXEを右クリックし、**「管理者として実行」** を選択してください。

### 3. 対象ドライブを選択

起動するとフォーマット可能な固定 / リムーバブルボリュームが表示されます。

Windowsが起動しているシステムドライブは一覧から除外されます。

表示例:

```text
Available target volumes:
  1) E:  476.94 GiB  FS=exFAT     512 B/sector  MUSICSSD
  2) F:  238.47 GiB  FS=NTFS      512 B/sector  BACKUP

Select target [1-2]:
```

FAT32化するドライブの番号を入力してEnterを押します。

**ドライブレター・容量・現在のファイルシステムを必ず確認してください。**

### 4. ボリュームラベルを入力

```text
Volume label [FAT32SSD]:
```

そのままEnterを押すと `FAT32SSD` になります。

ラベルはFAT32互換のため最大11文字相当です。英大文字、数字、スペース、`_`、`-` が使用できます。

小文字は大文字へ変換され、使用できない文字は除外されます。

### 5. クラスタサイズを選択

```text
Cluster size:  1) Auto  2) 4KB  3) 8KB  4) 16KB  5) 32KB  6) 64KB
Select [1]:
```

通常は何も入力せずEnter、または `1` を入力して **Auto** を使用してください。

256GB / 512GB / 1TB / 2TBクラスの大容量ストレージではAutoを推奨します。

大容量ではAutoは通常32 KiBクラスタを選択します。

2TBでは4 KiBクラスタはFAT32の最大クラスタ数を超えるため使用できません。Autoまたは8 / 16 / 32 / 64 KiBを使用してください。

### 6. 最終確認

フォーマット直前に対象情報が表示されます。

```text
WARNING: ALL DATA ON THE SELECTED VOLUME WILL BE ERASED.
Target : E:
Size   : 476.94 GiB (...)
Current: exFAT
Label  : FAT32SSD
Type exactly 'FORMAT E:' to continue:
```

Eドライブの場合は次の文字列を完全に同じ内容で入力します。

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

と入力してください。

確認文字列が一致しない場合は処理をキャンセルします。

### 7. フォーマット実行

確認後、対象ボリュームを排他ロックしてRAW I/Oを有効化し、FAT32構造を書き込みます。

主な処理内容:

- FAT32 Boot Sector / BPB作成
- FSInfo作成
- Backup Boot Sector作成
- Backup FSInfo作成
- FAT #1初期化
- FAT #2初期化
- Root Directory作成
- MBRの場合は可能ならpartition typeをFAT32 LBA `0x0C`へ変更

処理中はSSD / USBストレージを取り外したり、PCの電源を切ったりしないでください。

完了すると:

```text
[100%] Completed.

E: was formatted as FAT32 successfully.
```

と表示されます。

### 8. 完了後

エクスプローラーへすぐ反映されない場合は、外付けSSD / USBストレージを安全に取り外して接続し直してください。

またはWindowsの「ディスクの管理」からディスクを再スキャンしてください。

ドライブのプロパティで次のように表示されれば完了です。

```text
ファイル システム: FAT32
```

## exFATからFAT32へ変更する場合

最短手順:

```text
1. exFATのSSDを接続
2. FAT32SSDFormatter_console_v0.2.4.exeを起動
3. UACを許可
4. FS=exFAT と表示されている対象ドライブを選択
5. Volume labelを入力
6. Cluster sizeはAuto
7. FORMAT X: を入力
8. YES を入力
9. Completed. が表示されたらSSDを再接続
10. FAT32になっていることを確認
```

**exFAT上に存在していたファイルはすべて消去されます。**

## Important behavior

このツールは**物理ディスク全体を再パーティションするツールではありません**。

すでに存在し、ドライブレターが割り当てられているボリュームをFAT32として初期化します。

2TB SSDを1パーティションで使用したい場合は、必要に応じて事前にWindowsの「ディスクの管理」などでパーティションを作成してください。

接続先の古い機器によってはGPTではなくMBRが必要な場合があります。

## FAT32の制限

FAT32では1ファイルあたりの最大サイズは約4GBです。

また、FAT32として正常に作成できても、接続先のDJ機器、オーディオ機器、組み込み機器などに以下の制限がある場合があります。

- 最大ストレージ容量
- MBR / GPTの対応状況
- クラスタサイズ
- パーティション数
- USBストレージの種類

接続先機器の仕様も確認してください。

## 2TBについて

FAT32 BPBのTotal Sectorsフィールドは32-bitです。

512-byte logical sectorの場合、理論上表現できる上限は:

`2,199,023,255,040 bytes`

です。

一般的に「2TB」として販売されている約 `2,000,000,000,000 bytes` のSSDはこの範囲内です。

## Build

Goがインストールされた環境では、ソースからWindows x64向けEXEを作成できます。

Windows PowerShell:

```powershell
$env:GOOS="windows"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
go build -trimpath -ldflags="-s -w" -o FAT32SSDFormatter_console_v0.2.4.exe main.go
```

Linux / macOS:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags='-s -w' \
  -o FAT32SSDFormatter_console_v0.2.4.exe main.go
```

## Files

- `FAT32SSDFormatter_console_v0.2.4.exe` — Windows x64実行ファイル
- `main.go` — FAT32フォーマッタ本体のソースコード

## Disclaimer

本ツールはストレージへ直接書き込みを行います。

使用前に対象ドライブを十分確認してください。誤ったドライブを選択すると、そのドライブ上のデータは失われます。

本ツールの使用によるデータ消失や、接続先機器との非互換については利用者自身で確認してください。
