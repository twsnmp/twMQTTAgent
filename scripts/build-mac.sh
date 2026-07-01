#!/usr/bin/env bash
# =============================================================================
# scripts/build-mac.sh
# Build, sign, notarize, and staple the macOS Universal Binary for twMQTTAgent.
#
# Prerequisites:
#   - Xcode Command Line Tools installed (codesign, xcrun)
#   - Apple Developer ID Application certificate installed in Keychain
#   - Go installed
#
# Required environment variables:
#   APPLE_ID       - Your Apple ID email (e.g., developer@example.com)
#   APPLE_PASSWORD - App-specific password for notarytool
#   TEAM_ID        - Your Apple Developer Team ID (e.g., ABCDE12345)
#
# Optional environment variables:
#   SIGN_IDENTITY  - Codesign identity (default: "Developer ID Application: YOUR_NAME (TEAM_ID)")
# =============================================================================

set -euo pipefail

# --- Configuration -----------------------------------------------------------
APP_NAME="twMQTTAgent"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
DIST_DIR="dist"
BINARY_MAC="${DIST_DIR}/${APP_NAME}-mac"
BINARY_AMD64="${DIST_DIR}/${APP_NAME}-mac-amd64"
BINARY_ARM64="${DIST_DIR}/${APP_NAME}-mac-arm64"
ZIP_FILE="${DIST_DIR}/${APP_NAME}-mac.zip"

# Codesign identity — override via SIGN_IDENTITY env var if needed
SIGN_IDENTITY="${SIGN_IDENTITY:-Developer ID Application: YOUR_NAME (${TEAM_ID:-TEAM_ID})}"

# Validate required environment variables
require_env() {
  local var_name="$1"
  if [[ -z "${!var_name:-}" ]]; then
    echo "[ERROR] Required environment variable '${var_name}' is not set." >&2
    exit 1
  fi
}
require_env "APPLE_ID"
require_env "APPLE_PASSWORD"
require_env "TEAM_ID"

echo "========================================================"
echo "  twMQTTAgent macOS Build & Notarization"
echo "  Version : ${VERSION}"
echo "  Identity: ${SIGN_IDENTITY}"
echo "========================================================"

# --- Step 1: Compile Universal Binary ----------------------------------------
echo ""
echo "[1/5] Compiling macOS binaries..."
mkdir -p "${DIST_DIR}"

CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build \
  -trimpath \
  -ldflags="-s -w -X main.version=${VERSION}" \
  -o "${BINARY_AMD64}" .

CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
  -trimpath \
  -ldflags="-s -w -X main.version=${VERSION}" \
  -o "${BINARY_ARM64}" .

echo "    Merging into Universal Binary: ${BINARY_MAC}"
lipo -create -output "${BINARY_MAC}" "${BINARY_AMD64}" "${BINARY_ARM64}"
rm -f "${BINARY_AMD64}" "${BINARY_ARM64}"
echo "    Done."

# --- Step 2: Code Signing ----------------------------------------------------
echo ""
echo "[2/5] Signing with Developer ID..."
codesign \
  --force \
  --options runtime \
  --sign "${SIGN_IDENTITY}" \
  --timestamp \
  "${BINARY_MAC}"
echo "    Signed: ${BINARY_MAC}"

# Verify signature
codesign --verify --deep --strict --verbose=2 "${BINARY_MAC}"
echo "    Signature verified."

# --- Step 3: Package (zip for notarization) -----------------------------------
echo ""
echo "[3/5] Packaging into zip for notarization..."
rm -f "${ZIP_FILE}"
ditto -c -k --keepParent "${BINARY_MAC}" "${ZIP_FILE}"
echo "    Created: ${ZIP_FILE}"

# --- Step 4: Notarization -----------------------------------------------------
echo ""
echo "[4/5] Submitting for notarization (this may take a few minutes)..."
xcrun notarytool submit "${ZIP_FILE}" \
  --apple-id "${APPLE_ID}" \
  --password "${APPLE_PASSWORD}" \
  --team-id  "${TEAM_ID}" \
  --wait
echo "    Notarization complete."

# --- Step 5: Stapling ---------------------------------------------------------
# Note: Stapling is only possible for .app bundles, .dmg, or .pkg files.
# For a bare binary, notarization is sufficient; Gatekeeper checks Apple's
# servers online. If you need offline stapling, wrap the binary in a .dmg first.
echo ""
echo "[5/5] Stapling notarization ticket..."
if xcrun stapler staple "${BINARY_MAC}" 2>/dev/null; then
  echo "    Stapled successfully."
else
  echo "    [WARN] Stapling not supported for bare binaries."
  echo "    [WARN] Gatekeeper will verify online. Consider using a .dmg for offline distribution."
fi

echo ""
echo "========================================================"
echo "  Build complete!"
echo "  Output: ${BINARY_MAC}"
echo "  Zip   : ${ZIP_FILE}"
echo "========================================================"
