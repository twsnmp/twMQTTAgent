# twMQTTAgent

`twMQTTAgent` は、Go で書かれた軽量でクロスプラットフォーム対応のシステム監視エージェントです。システムメトリクス（CPU、メモリ、ロードアベレージ、ネットワーク、プロセス数）を収集し、JSON 形式で MQTT ブローカーにパブリッシュします。**TWSNMP** シリーズのモダンな拡張機能として設計されています。

[English](./README.md)

---

## 主な機能

- **システムメトリクスの監視:** 以下のリアルタイムなパフォーマンス指標を収集します。
  - CPU 使用率 (%)
  - メモリ使用率 (%)
  - ロードアベレージ
  - ネットワークトラフィック（送信/受信バイト数、送信/受信速度）
  - 実行中のプロセス数
- **ネットワークインターフェース統計:** `-if-interval` を設定することで、詳細なアダプター統計情報（MTU、MAC アドレス、稼働状態、エラー、パケットの送受信数など）を収集します。
- **ARP テーブルの監視:** `-arp-interval` を設定することで、定期的に ARP テーブルの情報を収集します。
- **MQTT 連携:** 収集したすべてのテレメトリデータを JSON ペイロードとして、指定された MQTT ブローカーへ送信します。
- **クロスプラットフォーム対応:** Windows、Linux、macOS で動作します。

## 設定と使用方法

`twMQTTAgent` はコマンドラインフラグを使用して設定します。

### CLI フラグ

| フラグ | デフォルト値 | 説明 |
|---|---|---|
| `-broker` | `tcp://localhost:1883` | MQTT ブローカーの URL (例: `tcp://192.168.1.1:1883`) |
| `-client-id` | `twMQTTAgent` | MQTT クライアント ID |
| `-topic` | `twMQTTAgent` | MQTT のベーストピック。ホスト名とデータ種別が自動的に付与されます (例: `<topic>/Monitor/<hostname>`) |
| `-interval` | `30` | システムメトリクス（Monitor）のパブリッシュ間隔（秒） |
| `-if-interval` | `0` | インターフェース統計（IF）のパブリッシュ間隔（秒）。`0` で無効化 |
| `-arp-interval` | `0` | ARP テーブル（Arp）のパブリッシュ間隔（秒）。`0` で無効化 |
| `-user` | | MQTT 接続用のユーザー名（任意） |
| `-password` | | MQTT 接続用のパスワード（任意） |
| `-hostname` | | トピックおよびペイロードで使用されるホスト名。未指定時はシステムのホスト名を使用 |

### 実行例

```bash
# 60秒間隔でシステムメトリクスを送信する基本実行
./twMQTTAgent -broker tcp://192.168.1.50:1883 -topic myhome/monitor -interval 60

# インターフェース統計と ARP テーブル監視を有効にして実行
./twMQTTAgent -broker tcp://192.168.1.50:1883 -if-interval 300 -arp-interval 600
```

---

## 開発とビルド

本プロジェクトでは、ツールバージョン管理およびタスクランナーとして `mise` を使用しています。

### 前提条件
- Go 1.21 以上

### ビルドコマンド

Go ツールチェーンを直接使用する場合:
```bash
# ローカルプラットフォーム向けにビルド
go build -o twMQTTAgent
```

`mise` を使用する場合:
```bash
# テスト用にローカルで実行
mise run run

# 現在のホストアーキテクチャ向けにビルド
mise run build:local

# Windows、Linux、macOS 向けにクロスコンパイル
mise run build:all

# macOS 向けのパッケージング、署名、および公証
mise run pkg:mac
```

---

## JSON ペイロード形式

### システムメトリクス (トピック: `<topic>/Monitor/<hostname>`)
```json
{
  "time": "2026-07-02T05:28:08+09:00",
  "host": "my-pc-name",
  "cpu": 12.5,
  "memory": 53.1,
  "load": 1.45,
  "sent": 1024,
  "recv": 2048,
  "tx_speed": 0.15,
  "rx_speed": 0.30,
  "process": 120
}
```

### インターフェース統計情報 (トピック: `<topic>/IF/<hostname>`)
```json
{
  "time": "2026-07-02T05:28:08+09:00",
  "host": "my-pc-name",
  "interfaces": [
    {
      "index": 1,
      "name": "eth0",
      "mtu": 1500,
      "mac": "00:11:22:33:44:55",
      "status": "up",
      "addrs": ["192.168.1.100/24"],
      "bytes_recv": 2048,
      "bytes_sent": 1024,
      "packets_recv": 20,
      "packets_sent": 15,
      "err_in": 0,
      "err_out": 0,
      "drop_in": 0,
      "drop_out": 0
    }
  ]
}
```

### ARP テーブル (トピック: `<topic>/Arp/<hostname>`)
```json
{
  "time": "2026-07-02T05:28:08+09:00",
  "host": "my-pc-name",
  "arp": [
    {
      "ip": "192.168.1.1",
      "mac": "00:11:22:33:44:55",
      "interface": "eth0",
      "type": "dynamic"
    }
  ]
}
```

---

## ライセンス

本プロジェクトは MIT ライセンスのもとで公開されています。詳細は [LICENSE](LICENSE) ファイルを参照してください。
