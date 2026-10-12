#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# 按云闪付交易记录页的版面画一张全虚构数据的长截图，供前端 OCR 与 unionpay_screenshot 解析器的单测使用
# 用法：python3 gen_synthetic.py <中文常规字体路径> <中文粗体字体路径> <输出路径>

import sys

from PIL import Image, ImageDraw, ImageFont

WIDTH = 1080
AMOUNT_RIGHT = 988
GRAY = (150, 150, 150)
BLACK = (30, 30, 30)

# (日期标头, [(渠道前缀, 商户, 金额, 卡行, 时间, 标签)])
DAYS = [
    ("10.09 周五", [
        ("(特约)", "虚构便利店", "-￥12.50", "招商[1111]", "21:05", ""),
        ("", "虚构咖啡馆", "-￥28.00", "滙豐[2222]", "18:40", ""),
        ("", "虚构酒店押金", "￥300.00", "滙豐[2222]", "15:00", "冻结"),
    ]),
    ("10.08 周四", [
        ("(美团)", "虚构面馆", "-￥36.80", "工行[3333]", "12:10", ""),
        ("", "虚构书店", "+￥9.90", "建行[4444]", "10:30", "退款"),
        ("", "信用卡还款", "-￥1,000.00", "招商[1111]", "09:00", ""),
    ]),
    ("10.05 周一", [
        ("", "虚构超市", "-￥158.20", "建行[4444]", "20:15", ""),
        ("", "虚构酒店押金", "￥300.00", "滙豐[2222]", "08:00", "解冻"),
        ("(特约)", "虚构加油站", "-￥200.00", "工行[3333]", "07:45", ""),
    ]),
    ("10.01 周四", [
        ("", "虚构电影院", "-￥75.00", "招商[1111]", "19:30", ""),
        ("", "虚构水果店", "-￥23.60", "滙豐[2222]", "17:20", ""),
    ]),
    ("9.30 周三", [
        ("", "虚构药店", "-￥45.30", "工行[3333]", "16:05", ""),
        ("(美团)", "虚构快餐店", "-￥19.90", "建行[4444]", "12:00", ""),
    ]),
    ("9.28 周一", [
        ("", "虚构洗衣店", "-￥30.00", "招商[1111]", "11:11", ""),
        ("", "虚构花店", "-￥88.00", "滙豐[2222]", "09:30", ""),
        ("", "虚构停车场", "-￥10.00", "工行[3333]", "08:20", ""),
    ]),
]


def main():
    font_path, bold_path, out_path = sys.argv[1], sys.argv[2], sys.argv[3]
    title = ImageFont.truetype(font_path, 50)
    big = ImageFont.truetype(font_path, 44)
    amount_font = ImageFont.truetype(bold_path, 42)
    mid = ImageFont.truetype(font_path, 38)
    small = ImageFont.truetype(font_path, 34)

    height = 800 + sum(130 + 210 * len(items) for _, items in DAYS) + 400
    img = Image.new("RGB", (WIDTH, height), (255, 255, 255))
    d = ImageDraw.Draw(img)

    d.text((434, 170), "交易记录", font=title, fill=BLACK)
    d.text((48, 305), "2026年10月→", font=mid, fill=BLACK)
    d.text((367, 305), "银联交易", font=big, fill=BLACK)
    for x, t in ((78, "全部"), (262, "支出"), (443, "收入")):
        d.text((x, 425), t, font=big, fill=BLACK)
    d.text((91, 595), "10月", font=big, fill=BLACK)
    d.text((91, 690), "支出 ￥1,234.56", font=small, fill=GRAY)

    y = 860
    for header, items in DAYS:
        d.text((104, y), header, font=small, fill=GRAY)
        y += 90
        for prefix, merchant, amount, card, hhmm, tag in items:
            x = 200
            if prefix:
                d.text((x, y), prefix, font=big, fill=BLACK)
                x += int(d.textlength(prefix, font=big)) + 4
            d.text((x, y), merchant, font=big, fill=BLACK)
            d.text((AMOUNT_RIGHT - d.textlength(amount, font=amount_font), y), amount, font=amount_font, fill=BLACK)
            d.text((200, y + 70), f"{card}·{hhmm}", font=small, fill=GRAY)
            if tag:
                d.text((915, y + 68), tag, font=mid, fill=GRAY)
            y += 210
        y += 40

    img.save(out_path, quality=90)


if __name__ == "__main__":
    main()
