#!/usr/bin/env python3
"""Draw docs/img/banner.png, the 1200x630 card link previews show.

Why this exists rather than an export from the SVG: this machine has no SVG
renderer and no way to install one, and a social card that only one laptop can
regenerate is a file nobody dares touch. Everything here is stdlib: zlib and
struct write the PNG, and the glyphs are a 5x7 bitmap font defined below.

The pixel font is not a compromise dressed up as a choice, but it is a happy
one: gnopie is a terminal tool and the card reads like one.

    python3 scripts/socialcard.py [out.png]
"""
import struct
import sys
import zlib

W, H = 1200, 630
BG = (0x0D, 0x11, 0x17)
GREEN = (0x7E, 0xE7, 0x87)
PURPLE = (0xD2, 0xA8, 0xFF)
FG = (0xC9, 0xD1, 0xD9)
DIM = (0x8B, 0x94, 0x9E)

# A 5x7 font, one string of five bits per row. Only the characters the card
# needs; add a glyph when you need one and the renderer will tell you which.
GLYPHS = {
    "a": ["00000", "00000", "01110", "00001", "01111", "10001", "01111"],
    "b": ["10000", "10000", "10110", "11001", "10001", "10001", "11110"],
    "c": ["00000", "00000", "01110", "10001", "10000", "10001", "01110"],
    "d": ["00001", "00001", "01101", "10011", "10001", "10001", "01111"],
    "e": ["00000", "00000", "01110", "10001", "11111", "10000", "01110"],
    "f": ["00110", "01001", "01000", "11100", "01000", "01000", "01000"],
    "g": ["00000", "01111", "10001", "10001", "01111", "00001", "01110"],
    "h": ["10000", "10000", "10110", "11001", "10001", "10001", "10001"],
    "i": ["00100", "00000", "01100", "00100", "00100", "00100", "01110"],
    "l": ["01100", "00100", "00100", "00100", "00100", "00100", "01110"],
    "m": ["00000", "00000", "11010", "10101", "10101", "10101", "10101"],
    "n": ["00000", "00000", "10110", "11001", "10001", "10001", "10001"],
    "o": ["00000", "00000", "01110", "10001", "10001", "10001", "01110"],
    "p": ["00000", "00000", "11110", "10001", "11110", "10000", "10000"],
    "r": ["00000", "00000", "10110", "11001", "10000", "10000", "10000"],
    "s": ["00000", "00000", "01111", "10000", "01110", "00001", "11110"],
    "t": ["01000", "01000", "11100", "01000", "01000", "01001", "00110"],
    "u": ["00000", "00000", "10001", "10001", "10001", "10011", "01101"],
    ",": ["00000", "00000", "00000", "00000", "00000", "00100", "01000"],
    ".": ["00000", "00000", "00000", "00000", "00000", "01100", "01100"],
    " ": ["00000"] * 7,
}


class Canvas:
    def __init__(self, w, h, bg):
        self.w, self.h = w, h
        self.px = bytearray(bytes(bg) * w * h)

    def set(self, x, y, c):
        if 0 <= x < self.w and 0 <= y < self.h:
            i = (y * self.w + x) * 3
            self.px[i : i + 3] = bytes(c)

    def blend(self, x, y, c, a):
        """a in 0..1, for the poor man's antialiasing the arcs need."""
        if not (0 <= x < self.w and 0 <= y < self.h) or a <= 0:
            return
        a = min(a, 1.0)
        i = (y * self.w + x) * 3
        for k in range(3):
            self.px[i + k] = int(self.px[i + k] * (1 - a) + c[k] * a)

    def text(self, s, x, y, c, scale):
        cx = x
        for ch in s:
            g = GLYPHS.get(ch)
            if g is None:
                raise SystemExit(f"socialcard.py: no glyph for {ch!r}; add one to GLYPHS")
            for ry, row in enumerate(g):
                for rx, bit in enumerate(row):
                    if bit == "1":
                        for dy in range(scale):
                            for dx in range(scale):
                                self.set(cx + rx * scale + dx, y + ry * scale + dy, c)
            cx += 6 * scale
        return cx

    def png(self):
        raw = b"".join(
            b"\x00" + bytes(self.px[y * self.w * 3 : (y + 1) * self.w * 3])
            for y in range(self.h)
        )

        def chunk(tag, data):
            return (
                struct.pack(">I", len(data))
                + tag
                + data
                + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
            )

        return (
            b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", self.w, self.h, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9))
            + chunk(b"IEND", b"")
        )


def ring(c, cx, cy, r, width, colour, a0, a1):
    """An arc of a circle, sampled densely enough to look solid."""
    import math

    steps = int(2 * math.pi * r * 4)
    for i in range(steps):
        t = a0 + (a1 - a0) * i / steps
        for w in range(-width // 2, width // 2 + 1):
            rr = r + w
            c.blend(int(cx + rr * math.cos(t)), int(cy + rr * math.sin(t)), colour, 1.0)


def wedge(c, cx, cy, r, colour, a0, a1, alpha):
    import math

    for yy in range(int(cy - r), int(cy + r) + 1):
        for xx in range(int(cx - r), int(cx + r) + 1):
            dx, dy = xx - cx, yy - cy
            if dx * dx + dy * dy > r * r:
                continue
            t = math.atan2(dy, dx) % (2 * math.pi)
            if a0 % (2 * math.pi) <= t <= a1 % (2 * math.pi):
                c.blend(xx, yy, colour, alpha)


def line(c, x0, y0, x1, y1, colour, width):
    steps = int(max(abs(x1 - x0), abs(y1 - y0)) * 2) or 1
    for i in range(steps + 1):
        x = x0 + (x1 - x0) * i / steps
        y = y0 + (y1 - y0) * i / steps
        for dy in range(-width // 2, width // 2 + 1):
            for dx in range(-width // 2, width // 2 + 1):
                if dx * dx + dy * dy <= (width // 2) ** 2:
                    c.blend(int(x) + dx, int(y) + dy, colour, 1.0)


def fit(s, x, scale, limit=W - 40):
    """Refuse to render a line that would run off the card."""
    end = x + len(s) * 6 * scale
    if end > limit:
        raise SystemExit(
            f"socialcard.py: {s!r} at scale {scale} ends at {end}px, past {limit}px. "
            f"Drop the scale or shorten the line."
        )


def main():
    import math

    out = sys.argv[1] if len(sys.argv) > 1 else "docs/img/banner.png"
    c = Canvas(W, H, BG)

    # The mark, same geometry as docs/img/logo.svg: a pie with one slice cut out
    # and served, and a needle that makes it a gauge.
    cx, cy, r = 250, 315, 150
    ring(c, cx, cy, r, 13, GREEN, math.radians(-60), math.radians(270))
    wedge(c, cx + 30, cy - 22, 130, GREEN, math.radians(-68), math.radians(-6), 0.45)
    ring(c, cx + 30, cy - 22, 130, 11, GREEN, math.radians(-68), math.radians(-6))
    line(c, cx, cy, cx - 62, cy - 74, PURPLE, 13)
    for yy in range(cy - 16, cy + 17):
        for xx in range(cx - 16, cx + 17):
            if (xx - cx) ** 2 + (yy - cy) ** 2 <= 16**2:
                c.set(xx, yy, PURPLE)

    # Scales are chosen so the longest line fits: a glyph is 6*scale wide, so
    # 32 characters at scale 4 would be 768px and run off a 1200px card from
    # x=460. fit() asserts that rather than leaving it to the eye.
    x0 = 460
    fit("gnopie", x0, 15)
    fit("httpie, but for gno.land", x0, 4)
    fit("the gas is measured, not guessed", x0, 3)

    c.text("gnopie", x0, 200, GREEN, 15)
    c.text("httpie, but for gno.land", x0 + 4, 345, FG, 4)
    c.text("the gas is measured, not guessed", x0 + 4, 410, DIM, 3)

    data = c.png()
    with open(out, "wb") as f:
        f.write(data)
    print(f"{out}: {W}x{H}, {len(data)} bytes")


if __name__ == "__main__":
    main()
