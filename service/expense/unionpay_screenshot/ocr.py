#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import json
import sys

def main():
    try:
        from rapidocr_onnxruntime import RapidOCR
    except ImportError as e:
        sys.stderr.write(f"rapidocr_onnxruntime import failed: {e}\n")
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
