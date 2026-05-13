#!/usr/bin/env python3
"""
生成 JSONL 格式的 SSE 响应测试数据 + 正确性基准文件。

输出:
  1. internal/resp/testdata/sse_events.jsonl   — SSE 事件（benchmark 用）
  2. internal/resp/testdata/correctness_golden.json — 每条事件的期望解析结果 + 累加汇总

每行格式: data: <json>\n
共 1000 条，按内容长度分 4 档：
  - small  (200~500B)    400 条 — 短回答 / 嵌入
  - medium (2~10KB)      300 条 — 典型对话
  - large  (20~100KB)    200 条 — 长文 / 代码生成
  - xlarge (100~500KB)   100 条 — 超长上下文

OpenAI 与 Anthropic 格式各占 50%。

用法:
  python3 scripts/gen_testdata.py
"""

import json
import os
import random
import string
import sys

# ── 配置 ──────────────────────────────────────────────────────────────────
TOTAL = 1000
DISTRIBUTION = [
    ("small", 400, 200, 500),
    ("medium", 300, 2_000, 10_000),
    ("large", 200, 20_000, 100_000),
    ("xlarge", 100, 100_000, 500_000),
]

OPENAI_MODELS = [
    "gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-3.5-turbo",
    "o1-preview", "o1-mini", "o3-mini",
    "deepseek-chat", "deepseek-reasoner",
    "qwen-max", "qwen-plus", "qwen-turbo",
]

ANTHROPIC_MODELS = [
    "claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022",
    "claude-3-opus-20240229", "claude-3-sonnet-20240229",
]

SEED = 42
OUTPUT_DIR = os.path.join(os.path.dirname(__file__), "..", "internal", "resp", "testdata")
OUTPUT_FILE = os.path.join(OUTPUT_DIR, "sse_events.jsonl")
GOLDEN_FILE = os.path.join(OUTPUT_DIR, "correctness_golden.json")


# ── 工具函数 ──────────────────────────────────────────────────────────────
def rand_content(length: int) -> str:
    """生成指定长度的伪 LLM 输出文本（含中英混合、代码片段）"""
    words = []
    total = 0
    while total < length:
        chunk = random.choice([
            "Hello ", "world ", "the ", "function ", "returns ",
            "这是一个", "测试响应", "包含中文",
            "def solve():", "    return 42", "```python\n",
            "import ", "from ", "class ", "async def ",
            "According to ", "the documentation, ",
            "In summary, ", "However, ",
        ])
        words.append(chunk)
        total += len(chunk)
    text = "".join(words)
    return text[:length]


def rand_tokens(base: int, variance: int = 50) -> int:
    return base + random.randint(0, variance)


# ── OpenAI 格式 ──────────────────────────────────────────────────────────
def make_openai_usage_chunk(model: str, content: str) -> dict:
    """OpenAI stream_options.include_usage=true 的最后一个 chunk"""
    prompt_tokens = rand_tokens(500, 2000)
    completion_tokens = rand_tokens(len(content) // 4, 500)
    cached = random.randint(0, prompt_tokens // 3)
    return {
        "id": f"chatcmpl-{random.randint(10**15, 10**16)}",
        "object": "chat.completion.chunk",
        "created": 1700000000 + random.randint(0, 100000),
        "model": model,
        "choices": [
            {
                "index": 0,
                "delta": {"content": content},
                "finish_reason": "stop",
            }
        ],
        "usage": {
            "prompt_tokens": prompt_tokens,
            "completion_tokens": completion_tokens,
            "total_tokens": prompt_tokens + completion_tokens,
            "prompt_tokens_details": {
                "cached_tokens": cached,
            },
        },
    }, {
        "model": model,
        "input": prompt_tokens,
        "output": completion_tokens,
        "cached": cached,
    }


# ── Anthropic 格式 ───────────────────────────────────────────────────────
def make_anthropic_usage_chunk(model: str, content: str) -> dict:
    """Anthropic message_delta（含 usage）"""
    input_tokens = rand_tokens(500, 2000)
    output_tokens = rand_tokens(len(content) // 4, 500)
    return {
        "type": "message_delta",
        "model": model,
        "delta": {
            "stop_reason": "end_turn",
            "stop_sequence": None,
        },
        "usage": {
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
        },
        "content": content,
    }, {
        "model": model,
        "input": input_tokens,
        "output": output_tokens,
        "cached": 0,
    }


# ── 生成 ──────────────────────────────────────────────────────────────────
def generate():
    random.seed(SEED)
    events: list[str] = []
    golden_results: list[dict] = []

    for label, count, min_len, max_len in DISTRIBUTION:
        for i in range(count):
            content_len = random.randint(min_len, max_len)
            content = rand_content(content_len)

            if random.random() < 0.5:
                model = random.choice(OPENAI_MODELS)
                obj, expected = make_openai_usage_chunk(model, content)
            else:
                model = random.choice(ANTHROPIC_MODELS)
                obj, expected = make_anthropic_usage_chunk(model, content)

            line = f"data: {json.dumps(obj, ensure_ascii=False)}"
            events.append(line)
            golden_results.append(expected)

    random.shuffle(events)
    # Shuffle golden_results with the same permutation as events
    # NOTE: We need to track the shuffle order
    # Actually, we generated events and golden_results in lockstep,
    # but then shuffled events. We need to shuffle golden_results the same way.
    # Let me redo this properly.

    # Redo: generate in lockstep, then shuffle together
    events.clear()
    golden_results.clear()

    pairs: list[tuple[str, dict]] = []
    random.seed(SEED)
    for label, count, min_len, max_len in DISTRIBUTION:
        for i in range(count):
            content_len = random.randint(min_len, max_len)
            content = rand_content(content_len)

            if random.random() < 0.5:
                model = random.choice(OPENAI_MODELS)
                obj, expected = make_openai_usage_chunk(model, content)
            else:
                model = random.choice(ANTHROPIC_MODELS)
                obj, expected = make_anthropic_usage_chunk(model, content)

            line = f"data: {json.dumps(obj, ensure_ascii=False)}"
            pairs.append((line, expected))

    random.shuffle(pairs)
    assert len(pairs) == TOTAL, f"Expected {TOTAL}, got {len(pairs)}"

    events = [p[0] for p in pairs]
    golden_results = [p[1] for p in pairs]

    # ── 写入 SSE 事件文件 ────────────────────────────────────────────────
    os.makedirs(OUTPUT_DIR, exist_ok=True)
    with open(OUTPUT_FILE, "w", encoding="utf-8") as f:
        for line in events:
            f.write(line + "\n")

    # ── 计算累加汇总 ────────────────────────────────────────────────────
    total_input = sum(r["input"] for r in golden_results)
    total_output = sum(r["output"] for r in golden_results)
    total_cached = sum(r["cached"] for r in golden_results)

    golden = {
        "file": "sse_events.jsonl",
        "count": len(events),
        "results": golden_results,
        "summary": {
            "total_input": total_input,
            "total_output": total_output,
            "total_cached": total_cached,
        },
    }

    with open(GOLDEN_FILE, "w", encoding="utf-8") as f:
        json.dump(golden, f, indent=2)

    # ── 统计 ────────────────────────────────────────────────────────────
    sizes = [len(line.encode()) for line in events]
    print(f"Generated {len(events)} SSE events → {OUTPUT_FILE}")
    print(f"  File size: {os.path.getsize(OUTPUT_FILE) / 1024:.1f} KB")
    print(f"  Line size: min={min(sizes)}B  median={sorted(sizes)[len(sizes)//2]}B  max={max(sizes)}B")
    print(f"Golden results → {GOLDEN_FILE}")
    print(f"  Summary: input={total_input}  output={total_output}  cached={total_cached}")


if __name__ == "__main__":
    generate()
