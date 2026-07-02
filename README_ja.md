# twMQTTAgent

`twMQTTAgent` は、Go で書かれた軽量でクロスプラットフォーム対応のシステム監視エージェントです。システムメトリクス（CPU、メモリ、ロードアベレージ、ネットワーク、プロセス数）を収集し、JSON 形式で MQTT ブローカーにパブリッシュします。**TWSNMP** シリーズのモダンな拡張機能として設計されています。

[English](./README.md)

---

## 🤖 AI エージェント用指示書: 実装仕様

AI アシスタントへ：以下の詳細な仕様に基づいて、Go のソースコード、`mise` の設定（`mise.toml`）、タスクスクリプト、および GitHub Actions のワークフローを含む完全なプロジェクト構造を生成してください。

### 1. Go アプリケーションのコア要件
- **開発言語:** Go (Golang) 1.21+
- **メトリクスライブラリ:** `github.com/shirou/gopsutil/v4`（特に `cpu`、`load`、`mem`、`net`、`process` パッケージ）を使用。
- **MQTT ライブラリ:** `github.com/eclipse/paho.mqtt.golang` を使用。
- **対象 OS:** Windows、Linux、macOS へのクロスコンパイルをサポート（適切な箇所で `CGO_ENABLED=0` とする）。
- **CLI フラグ:** `--broker`、`--client-id`、`--topic`、`--interval`、`--if-interval`、`--arp-interval`、`--user`、`--password` の標準フラグ。
- **ペイロード:** TWSNMP のデータ構造に適合する高精度な JSON ペイロード。

---

### 2. 環境およびタスク管理 (`mise.toml`)
開発環境およびビルドタスクを管理するための `mise.toml` ファイルを生成してください。

- **ツール:**
  - `go` (最新の 1.21+ または 1.22+)
- **タスク (`[tasks]`):**
  - `run`: テスト用にローカルでエージェントを実行します。
  - `build:local`: 現在のローカルホストのアーキテクチャ向けにバイナリをビルドします。
  - `build:all`: すべてのクロスコンパイルタスクを実行します。
  - `pkg:mac`: ローカル macOS でのパッケージング、署名、および公証スクリプトを実行します。

---

### 3. CI/CD & リリース: GitHub Actions (`.github/workflows/release.yml`)
新しい Git タグ（例: `v*`）がプッシュされたときにトリガーされる GitHub Actions ワークフローファイルを生成してください。

- **ジョブとマトリックス:**
  - **Windows (amd64)** および **Linux (amd64, arm64)** 向けのバイナリをコンパイルしてリリースすること。
  - **成果物（アーティファクト）:** `twMQTTAgent-windows-amd64.exe`、`twMQTTAgent-linux-amd64`、`twMQTTAgent-linux-arm64`。
  - **リリース:** 標準のアクションステップ（例: `softprops/action-gh-release`）を使用して、自動的に GitHub Release を作成し、これらのアセットをアップロードすること。

---

### 4. ローカル macOS パッケージング・署名・公証スクリプト (`scripts/build-mac.sh`)
macOS の署名および公証には、Apple Developer 証明書と外部 API との通信が必要となるため、このプロセスは `mise run pkg:mac` を介して実行されるローカルの Bash スクリプトとして記述してください。

スクリプトは以下を処理する必要があります：
1. **コンパイル:** macOS 向けのユニバーサルバイナリ（または `amd64`/`arm64` 個別のターゲット）をビルドする。
2. **パッケージング:** 必要に応じて `.dmg` または `.pkg` ラッパーを作成するか、アプリバンドルを用意する。
3. **署名 (`codesign`):**
   `codesign --force --options runtime --sign "Developer ID Application: YOUR_NAME (TEAM_ID)" ./twMQTTAgent-mac` を使用する。
4. **公証 (`xcrun notarytool`):**
   - バイナリを `.zip` ファイルに圧縮する。
   - 認証情報用の環境変数（`APPLE_ID`、`APPLE_PASSWORD`、`TEAM_ID`）を利用して `xcrun notarytool submit` で送信する。
5. **ステープル (`xcrun stapler`):** アプリケーション/パッケージに公証チケットをステープルする。

*注意: スクリプトには環境変数のプレースホルダーを用意し、ローカルの Mac 上で安全に実行できるようにしてください。*

---

### 5. 想定される JSON ペイロード形式
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

### 6. インターフェース統計情報の JSON ペイロード形式 (トピック: `<topic>/IF/<hostname>`)
`--if-interval` に 0 以外の値が設定されている場合にパブリッシュされます。
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

### 7. ARP テーブルの JSON ペイロード形式 (トピック: `<topic>/Arp/<hostname>`)
`--arp-interval` に 0 以外の値が設定されている場合にパブリッシュされます。
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
