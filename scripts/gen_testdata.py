#!/usr/bin/env python3
"""
生成 JSONL 格式的 SSE 响应测试数据，用于 benchmark 压测。

每行格式: data: <json>\n
共 1000 条，按内容长度分 4 档：
  - small  (200~500B)    400 条 — 短回答 / 嵌入
  - medium (2~10KB)      300 条 — 典型对话
  - large  (20~100KB)    200 条 — 长文 / 代码生成
  - xlarge (100~500KB)   100 条 — 超长上下文

OpenAI 与 Anthropic 格式各占 50%。

用法:
  python3 scripts/gen_testdata.py
  # 输出: internal/resp/testdata/sse_events.jsonl
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
                "cached_tokens": random.randint(0, prompt_tokens // 3),
            },
        },
    }


# ── Anthropic 格式 ───────────────────────────────────────────────────────
def make_anthropic_usage_chunk(model: str, content: str) -> dict:
    """Anthropic message_delta（含 usage）"""
    input_tokens = rand_tokens(500, 2000)
    output_tokens = rand_tokens(len(content) // 4, 500)
    return {
        "type": "message_delta",
        "delta": {
            "stop_reason": "end_turn",
            "stop_sequence": None,
        },
        "usage": {
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
        },
        "content": content,  # 模拟附带的大段内容
    }


# ── 生成 ──────────────────────────────────────────────────────────────────
def generate():
    random.seed(SEED)
    events: list[str] = []

    for label, count, min_len, max_len in DISTRIBUTION:
        for i in range(count):
            content_len = random.randint(min_len, max_len)
            content = rand_content(content_len)

            if random.random() < 0.5:
                model = random.choice(OPENAI_MODELS)
                obj = make_openai_usage_chunk(model, content)
            else:
                model = random.choice(ANTHROPIC_MODELS)
                obj = make_anthropic_usage_chunk(model, content)

            line = f"data: {json.dumps(obj, ensure_ascii=False)}"
            events.append(line)

    random.shuffle(events)
    assert len(events) == TOTAL, f"Expected {TOTAL}, got {len(events)}"

    os.makedirs(OUTPUT_DIR, exist_ok=True)
    with open(OUTPUT_FILE, "w", encoding="utf-8") as f:
        for line in events:
            f.write(line + "\n")

    # 统计
    sizes = [len(line.encode()) for line in events]
    print(f"Generated {TOTAL} SSE events → {OUTPUT_FILE}")
    print(f"  File size: {os.path.getsize(OUTPUT_FILE) / 1024:.1f} KB")
    print(f"  Line size: min={min(sizes)}B  median={sorted(sizes)[len(sizes)//2]}B  max={max(sizes)}B")


if __name__ == "__main__":
    generate()
