#!/usr/bin/env bash
# scripts/install_tools.sh — 安装 Go 静态分析工具链
set -euo pipefail

GOBIN="${GOBIN:-$(go env GOPATH)/bin}"
echo "GOBIN: $GOBIN"

install() {
    local name="$1"
    local pkg="$2"
    if command -v "$name" &>/dev/null; then
        echo "[skip] $name 已安装: $(command -v "$name")"
    else
        echo "[install] $name ..."
        go install "$pkg@latest"
        echo "[done]  $name → $(command -v "$name" || echo "$GOBIN/$name")"
    fi
}

install "staticcheck"   "honnef.co/go/tools/cmd/staticcheck"
install "golangci-lint" "github.com/golangci/golangci-lint/cmd/golangci-lint"
install "govulncheck"   "golang.org/x/vuln/cmd/govulncheck"
install "goimports"     "golang.org/x/tools/cmd/goimports"

echo ""
echo "✓ 全部安装完成"
echo "  确保 \$GOBIN 在 PATH 中: export PATH=\"\$GOBIN:\$PATH\""
