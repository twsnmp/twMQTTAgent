# twMQTTAgent

`twMQTTAgent` is a lightweight, cross-platform system monitoring agent written in Go. It collects system metrics (CPU, Memory, Load, Network, Processes) and publishes them to an MQTT broker in JSON format. It is designed as a modern extension for the **TWSNMP** series.

---

## 🤖 AI Agent Instruction: Implementation Specification

Dear AI Assistant, please generate the complete project structure including Go source code, `mise` configuration (`mise.toml`), task scripts, and GitHub Actions workflow based on the following detailed specifications.

### 1. Core Go Application Requirements
- **Language:** Go (Golang) 1.21+
- **Metrics Library:** Use `github.com/shirou/gopsutil/v4` (specifically `cpu`, `load`, `mem`, `net`, and `process` packages).
- **MQTT Library:** Use `github.com/eclipse/paho.mqtt.golang`.
- **Target OS:** Must support cross-compilation for Windows, Linux, and macOS (`CGO_ENABLED=0` where applicable).
- **CLI Flags:** Standard flags for `--broker`, `--client-id`, `--topic`, `--interval`, `--user`, and `--password`.
- **Payload:** High-fidelity JSON payload matching TWSNMP data structures (specifically matching the `twBlueScan` monitor format).

---

### 2. Environment & Task Management (`mise.toml`)
Please generate a `mise.toml` file to manage the development environment and build tasks.

- **Tools:**
  - `go` (latest 1.21+ or 1.22+)
- **Tasks (`[tasks]`):**
  - `run`: Runs the agent locally for testing.
  - `build:local`: Builds the binary for the current local host architecture.
  - `build:all`: Runs all cross-compilation tasks.
  - `pkg:mac`: Triggers the local macOS packaging, signing, and notarization script.

---

### 3. CI/CD & Release: GitHub Actions (`.github/workflows/release.yml`)
Please generate a GitHub Actions workflow file that triggers when a new Git tag (e.g., `v*`) is pushed.

- **Jobs & Matrix:**
  - Must compile and release binaries for **Windows (amd64)** and **Linux (amd64, arm64)**.
  - **Output Artifacts:** `twMQTTAgent-windows-amd64.exe`, `twMQTTAgent-linux-amd64`, `twMQTTAgent-linux-arm64`.
  - **Release:** Automatically create a GitHub Release and upload these assets using standard action steps (e.g., `softprops/action-gh-release`).

---

### 4. Local macOS Packaging, Signing & Notarization Script (`scripts/build-mac.sh`)
Since macOS signing and notarization require Apple Developer certificates and external API communication, this process must be written into a local bash script executed via `mise run pkg:mac`. 

The script must handle:
1. **Compilation:** Build a Universal Binary (or separate `amd64`/`arm64` targets) for macOS.
2. **Packaging:** Create a `.dmg` or `.pkg` wrapper if necessary, or prepare the app bundle.
3. **Signing (`codesign`):** - Use `codesign --force --options runtime --sign "Developer ID Application: YOUR_NAME (TEAM_ID)" ./twMQTTAgent-mac`
4. **Notarization (`xcrun notarytool`):**
   - Compress the binary into a `.zip` file.
   - Submit via `xcrun notarytool submit` utilizing environment variables for credentials (`APPLE_ID`, `APPLE_PASSWORD`, `TEAM_ID`).
5. **Stapling (`xcrun stapler`):** Staple the notarization ticket to the application/package.

*Note: Provide the script with placeholders for environment variables so it can be executed safely on a local Mac.*

---

### 5. Expected JSON Payload Format
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