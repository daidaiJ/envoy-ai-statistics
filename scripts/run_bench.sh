#!/usr/bin/env bash
# scripts/run_bench.sh — 一键运行三种 JSON 解析方案的 benchmark 并生成对比报告
#
# 依赖: go ≥1.22, python3, lscpu, free
# 输出: build/bench_report_<timestamp>.md

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

# ── 帮助信息 ──────────────────────────────────────────────────────────────
usage() {
    cat <<'USAGE'
Usage: scripts/run_bench.sh [command] [options]

Commands:
  bench [options]   Run benchmark + correctness verification (default)
  verify            Run correctness verification only
  clean             Remove generated test data and build artifacts
  help              Show this help message

Bench options (passed through to `go test -bench`):
  -benchtime=10s    Duration per benchmark (default: 5s)
  -count=3          Run each benchmark N times

Examples:
  scripts/run_bench.sh                       # benchmark + verify
  scripts/run_bench.sh bench -benchtime=10s   # benchmark (10s each)
  scripts/run_bench.sh verify                 # verify correctness only
  scripts/run_bench.sh clean                  # remove test data & reports
USAGE
}

# ── 确保测试数据存在（含 golden 文件）─────────────────────────────────────
ensure_testdata() {
    if [ ! -f internal/resp/testdata/sse_events.jsonl ] || [ ! -f internal/resp/testdata/correctness_golden.json ]; then
        echo ">> Generating test data + golden file..."
        python3 scripts/gen_testdata.py
    fi
}

# ── 子命令: clean ─────────────────────────────────────────────────────────
cmd_clean() {
    local removed=0

    for f in internal/resp/testdata/sse_events.jsonl internal/resp/testdata/correctness_golden.json; do
        if [ -f "$f" ]; then
            rm -f "$f"
            echo ">> Removed: $f"
            removed=1
        fi
    done

    if [ -d build ]; then
        rm -rf build
        echo ">> Removed: build/"
        removed=1
    fi

    if [ "$removed" -eq 0 ]; then
        echo ">> Nothing to clean."
    else
        echo ">> Done."
    fi
}

# ── 正确性验证 ────────────────────────────────────────────────────────────
cmd_verify() {
    ensure_testdata

    echo ">> Running correctness verification against golden file..."
    local has_fail=0

    for entry in "default|" "gjson|gjson" "sonic|sonicjson"; do
        IFS='|' read -r label tag <<< "$entry"
        local tag_flag=""
        if [ -n "$tag" ]; then
            tag_flag="-tags $tag"
        fi
        echo ""
        echo "--- ${label} ---"
        # shellcheck disable=SC2086
        if go test -run="TestExtractCorrectness_${label}" -v ${tag_flag} ./internal/resp/ 2>&1; then
            :
        else
            has_fail=1
        fi
    done

    echo ""
    if [ "$has_fail" -eq 0 ]; then
        echo ">> ✅ All implementations match golden file."
    else
        echo ">> ❌ Some implementations failed! See errors above."
        return 1
    fi
}

# ── 子命令: bench ─────────────────────────────────────────────────────────
cmd_bench() {
    local bench_args="${*:-"-benchtime=5s"}"
    local timestamp
    timestamp="$(date +%Y%m%d_%H%M%S)"
    local report="build/bench_report_${timestamp}.md"

    mkdir -p build
    ensure_testdata

    # ── 采集环境信息 ─────────────────────────────────────────────────────
    echo ">> Collecting system info..."

    local go_version go_env
    go_version="$(go version)"
    go_env="$(go env GOARCH GOOS CGO_ENABLED 2>/dev/null || echo 'N/A')"

    local cpu_model cpu_cores mem_total mem_avail
    cpu_model="$(lscpu 2>/dev/null | grep 'Model name' | sed 's/.*:\s*//' || echo 'N/A')"
    cpu_cores="$(nproc 2>/dev/null || echo 'N/A')"
    mem_total="$(free -h 2>/dev/null | awk '/^Mem:/{print $2}' || echo 'N/A')"
    mem_avail="$(free -h 2>/dev/null | awk '/^Mem:/{print $7}' || echo 'N/A')"

    local os_info kernel
    os_info="$(uname -srm 2>/dev/null || echo 'N/A')"
    kernel="$(uname -r 2>/dev/null || echo 'N/A')"

    local gjson_ver sonic_ver jsoniter_ver
    gjson_ver="$(go list -m -json github.com/tidwall/gjson 2>/dev/null | grep '"Version"' | head -1 | sed 's/.*"Version": "//;s/".*//' || echo 'N/A')"
    sonic_ver="$(go list -m -json github.com/bytedance/sonic 2>/dev/null | grep '"Version"' | head -1 | sed 's/.*"Version": "//;s/".*//' || echo 'N/A')"
    jsoniter_ver="$(go list -m -json github.com/json-iterator/go 2>/dev/null | grep '"Version"' | head -1 | sed 's/.*"Version": "//;s/".*//' || echo 'N/A')"

    local data_lines data_size
    data_lines="$(wc -l < internal/resp/testdata/sse_events.jsonl | tr -d ' ')"
    data_size="$(du -h internal/resp/testdata/sse_events.jsonl | cut -f1)"

    # ── 写入报告头部 ─────────────────────────────────────────────────────
    cat > "$report" <<EOF
# JSON 解析方案 Benchmark 报告

> 生成时间: $(date '+%Y-%m-%d %H:%M:%S %Z')

## 环境信息

| 项目 | 值 |
|------|-----|
| OS | ${os_info} |
| Kernel | ${kernel} |
| CPU | ${cpu_model} (${cpu_cores} cores) |
| Memory | ${mem_total} (available: ${mem_avail}) |
| Go | ${go_version} |
| GOARCH / GOOS / CGO | ${go_env} |

## 依赖版本

| 库 | 版本 |
|----|------|
| json-iterator/go | ${jsoniter_ver} |
| tidwall/gjson | ${gjson_ver} |
| bytedance/sonic | ${sonic_ver} |

## 测试数据

| 项目 | 值 |
|------|-----|
| 文件 | \`internal/resp/testdata/sse_events.jsonl\` |
| 条数 | ${data_lines} |
| 大小 | ${data_size} |

## Benchmark 参数

\`\`\`
${bench_args}
\`\`\`

---

EOF

    # ── 运行 benchmark ───────────────────────────────────────────────────
    run_single_bench() {
        local label="$1"
        local tag="$2"
        local tag_flag=""
        if [ -n "$tag" ]; then
            tag_flag="-tags $tag"
        fi

        echo ">> Benchmarking: ${label} ..."
        echo "## ${label}" >> "$report"
        echo "" >> "$report"
        echo '```' >> "$report"

        # shellcheck disable=SC2086
        go test -bench=. -benchmem -run=^$ ${tag_flag} ${bench_args} ./internal/resp/ 2>&1 | tee -a "$report"

        echo '```' >> "$report"
        echo "" >> "$report"
    }

    run_single_bench "default (json-iterator)" ""
    run_single_bench "gjson (path extract)" "gjson"
    run_single_bench "sonicjson (sonic JIT)" "sonicjson"

    # ── 正确性验证 ───────────────────────────────────────────────────────
    echo "" >> "$report"
    echo "## 正确性验证 (vs golden file)" >> "$report"
    echo "" >> "$report"

    local has_fail=0
    echo '| 实现 | 结果 |' >> "$report"
    echo '|------|------|' >> "$report"

    for entry in "default|" "gjson|gjson" "sonic|sonicjson"; do
        IFS='|' read -r label tag <<< "$entry"
        local tag_flag=""
        if [ -n "$tag" ]; then
            tag_flag="-tags $tag"
        fi
        echo "   Verifying: ${label} ..."
        # shellcheck disable=SC2086
        if go test -run="TestExtractCorrectness_${label}" -v ${tag_flag} ./internal/resp/ 2>&1 | tee /dev/stderr | grep -q "^--- PASS"; then
            echo "| ${label} | ✅ PASS |" >> "$report"
        else
            echo "| ${label} | ❌ FAIL |" >> "$report"
            has_fail=1
        fi
    done

    # ── 汇总 ─────────────────────────────────────────────────────────────
    echo "" >> "$report"
    echo "---" >> "$report"
    echo "" >> "$report"
    echo "_Report generated by \`scripts/run_bench.sh\`_" >> "$report"

    echo ""
    echo ">> Report saved to: ${report}"

    if [ "$has_fail" -ne 0 ]; then
        echo ">> ⚠️  Correctness verification failed!"
        return 1
    fi
}

# ── 主入口 ────────────────────────────────────────────────────────────────
case "${1:-bench}" in
    bench)
        shift || true
        cmd_bench "$@"
        ;;
    verify)
        cmd_verify
        ;;
    clean)
        cmd_clean
        ;;
    help|-h|--help)
        usage
        ;;
    -*)
        cmd_bench "$@"
        ;;
    *)
        echo "Unknown command: $1" >&2
        echo "" >&2
        usage >&2
        exit 1
        ;;
esac
