#!/usr/bin/env python3
"""Draw the opx app icon: a padlock body with a fingerprint inside it.

Renders a single 1024×1024 white-on-transparent PNG. The Go side recolors
this at runtime based on the macOS appearance setting (dark/light) before
handing it to AppleScript.

Designed at 1024×1024 with stroke widths that hold up at 32px (the size
macOS renders inside `display dialog`).
"""
from PIL import Image, ImageDraw
import os
import sys

SIZE = 1024
FG = (255, 255, 255, 255)  # opaque white — Go recolors RGB at runtime
BG = (0, 0, 0, 0)          # transparent


def draw(size: int = SIZE) -> Image.Image:
    s = size / 1024.0  # scale factor — design is in 1024 units
    img = Image.new("RGBA", (size, size), BG)
    d = ImageDraw.Draw(img)

    # ---- padlock body ------------------------------------------------------
    body_w, body_h = 640, 540
    body_x = (1024 - body_w) // 2
    body_y = 440
    body_r = 90
    body_stroke = 44
    d.rounded_rectangle(
        [body_x * s, body_y * s, (body_x + body_w) * s, (body_y + body_h) * s],
        radius=body_r * s,
        outline=FG,
        width=int(body_stroke * s),
    )

    # ---- shackle -----------------------------------------------------------
    sh_w, sh_h = 380, 380
    sh_x = (1024 - sh_w) // 2
    sh_y = 180
    sh_stroke = 60
    d.arc(
        [sh_x * s, sh_y * s, (sh_x + sh_w) * s, (sh_y + sh_h) * s],
        start=180, end=360,
        fill=FG,
        width=int(sh_stroke * s),
    )
    leg_top = sh_y + sh_h // 2
    leg_bot = body_y + 30
    leg_half = sh_stroke // 2
    for cx in (sh_x, sh_x + sh_w):
        d.rectangle(
            [(cx - leg_half) * s, leg_top * s, (cx + leg_half) * s, leg_bot * s],
            fill=FG,
        )

    # ---- fingerprint inside the body --------------------------------------
    cx, cy = 512, body_y + body_h // 2 + 20
    ridge_stroke = 38
    ridges = [
        (90,  195, 345),
        (170, 185, 355),
        (245, 180, 360),
    ]
    for r, a0, a1 in ridges:
        d.arc(
            [(cx - r) * s, (cy - r) * s, (cx + r) * s, (cy + r) * s],
            start=a0, end=a1,
            fill=FG,
            width=int(ridge_stroke * s),
        )
    dot_r = 28
    d.ellipse(
        [(cx - dot_r) * s, (cy - dot_r) * s, (cx + dot_r) * s, (cy + dot_r) * s],
        fill=FG,
    )

    return img


if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else "opx.png"
    os.makedirs(os.path.dirname(out) or ".", exist_ok=True)
    draw(SIZE).save(out)
