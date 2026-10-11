#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import json
import sys
from importlib import metadata

def main():
    try:
        from rapidocr_onnxruntime import RapidOCR
    except ImportError as e:
        sys.stderr.write(f"rapidocr_onnxruntime import failed: {e}\n")
        sys.exit(1)

    # 1.3 起默认把长边缩到 2000，长截图会被缩糊，版本必须跟 requirements.txt 锁定的一致
    expected = sys.argv[1] if len(sys.argv) > 1 else ""
    installed = metadata.version("rapidocr_onnxruntime")
    if installed != expected:
        sys.stderr.write(f"rapidocr_onnxruntime version mismatch: installed {installed}, expected {expected}\n")
        sys.exit(1)

    image_bytes = sys.stdin.buffer.read()
    if not image_bytes:
        print("[]")
        return

    try:
        engine = RapidOCR()
        result, _ = engine(image_bytes)
    except Exception as e:
        sys.stderr.write(f"RapidOCR execution failed: {e}\n")
        sys.exit(1)

    blocks = []
    if result:
        for box, text, score in result:
            blocks.append({
                "text": text.strip(),
                "x": float(box[0][0]),
                "y": float(box[0][1]),
                "score": float(score),
            })

    print(json.dumps(blocks, ensure_ascii=False))

if __name__ == "__main__":
    main()
