import argparse
from pathlib import Path
from PIL import Image, ImageDraw

DEFAULT_EU4_DIR = Path(r"D:\Steam\steamapps\common\Europa Universalis IV")
ROOT_DIR = Path(__file__).resolve().parent.parent
DEFAULT_OUT_DIR = ROOT_DIR / "assets" / "dev_portal"
DEFAULT_FLAGS_DIR = ROOT_DIR / "assets" / "flags"


def make_badge(size: int, top_rgb: tuple[int, int, int], bot_rgb: tuple[int, int, int], border_rgb: tuple[int, int, int]) -> Image.Image:
    img = Image.new("RGBA", (size, size))
    px = img.load()
    for y in range(size):
        t = y / max(1, size - 1)
        r = int(top_rgb[0] * (1 - t) + bot_rgb[0] * t)
        g = int(top_rgb[1] * (1 - t) + bot_rgb[1] * t)
        b = int(top_rgb[2] * (1 - t) + bot_rgb[2] * t)
        for x in range(size):
            px[x, y] = (r, g, b, 255)

    draw = ImageDraw.Draw(img)
    pad1 = int(size * 0.03)
    pad2 = int(size * 0.05)
    draw.rectangle([pad1, pad1, size - 1 - pad1, size - 1 - pad1], outline=(*border_rgb, 210), width=max(2, size // 128))
    draw.rectangle([pad2, pad2, size - 1 - pad2, size - 1 - pad2], outline=(*border_rgb, 90), width=max(1, size // 256))
    return img


def composite_centered(bg: Image.Image, fg: Image.Image, target_ratio: float = 0.72) -> Image.Image:
    w, h = bg.size
    max_w = int(w * target_ratio)
    max_h = int(h * target_ratio)
    fw, fh = fg.size
    scale = min(max_w / fw, max_h / fh)
    nw = max(1, int(round(fw * scale)))
    nh = max(1, int(round(fh * scale)))
    fg_resized = fg.convert("RGBA").resize((nw, nh), Image.Resampling.LANCZOS)

    out = bg.copy()
    ox = (w - nw) // 2
    oy = (h - nh) // 2
    out.alpha_composite(fg_resized, (ox, oy))
    return out


def export_dev_portal_assets(eu4_dir: Path, out_dir: Path) -> None:
    out_dir.mkdir(parents=True, exist_ok=True)
    size = 512

    logo_src = eu4_dir / "gfx" / "interface" / "Eu4_logo.dds"
    with Image.open(logo_src) as logo:
        bg = make_badge(size, (22, 34, 58), (10, 15, 28), (212, 175, 55))
        comp = composite_centered(bg, logo, target_ratio=0.86)
        comp.save(out_dir / "eu4_logo.png", "PNG")

    # icon_age_l.dds is a 4-frame strip (52x52 each)
    ages_src = eu4_dir / "gfx" / "interface" / "ages" / "icon_age_l.dds"
    age_names = [
        "age_of_discovery",
        "age_of_reformation",
        "age_of_absolutism",
        "age_of_revolutions",
    ]
    with Image.open(ages_src) as sheet:
        frame_w = sheet.width // 4
        frame_h = sheet.height
        for idx, name in enumerate(age_names):
            crop = sheet.crop((idx * frame_w, 0, (idx + 1) * frame_w, frame_h))
            bg = make_badge(size, (30, 26, 22), (14, 12, 10), (205, 164, 52))
            comp = composite_centered(bg, crop, target_ratio=0.74)
            comp.save(out_dir / f"{name}.png", "PNG")

    status_specs = [
        ("at_war", eu4_dir / "gfx" / "interface" / "icon_diplomacy_war.dds", (56, 16, 18), (24, 8, 10), (205, 70, 60)),
        ("ironman", eu4_dir / "gfx" / "interface" / "ironman_icon.dds", (28, 36, 48), (14, 18, 24), (170, 185, 205)),
    ]
    for name, src_path, top_rgb, bot_rgb, border_rgb in status_specs:
        with Image.open(src_path) as icon:
            bg = make_badge(size, top_rgb, bot_rgb, border_rgb)
            comp = composite_centered(bg, icon, target_ratio=0.72)
            comp.save(out_dir / f"{name}.png", "PNG")

    # button_pause.dds is a cut-off corner piece so just draw a pause icon
    paused_bg = make_badge(size, (36, 32, 22), (16, 14, 10), (212, 175, 55))
    draw = ImageDraw.Draw(paused_bg)
    c_pad = int(size * 0.14)
    draw.ellipse([c_pad, c_pad, size - c_pad, size - c_pad], fill=(122, 22, 24, 255), outline=(212, 175, 55, 255), width=14)
    draw.ellipse([c_pad + 18, c_pad + 18, size - c_pad - 18, size - c_pad - 18], outline=(245, 214, 120, 120), width=4)
    bar_w, bar_h = 46, 172
    gap = 40
    cx, cy = size // 2, size // 2
    y0, y1 = cy - bar_h // 2, cy + bar_h // 2
    draw.rounded_rectangle([cx - gap // 2 - bar_w, y0, cx - gap // 2, y1], radius=8, fill=(238, 203, 104, 255), outline=(92, 68, 16, 255), width=4)
    draw.rounded_rectangle([cx + gap // 2, y0, cx + gap // 2 + bar_w, y1], radius=8, fill=(238, 203, 104, 255), outline=(92, 68, 16, 255), width=4)
    paused_bg.save(out_dir / "paused.png", "PNG")

    print(f"exported 8 icons to {out_dir}")


def export_flags(eu4_dir: Path, flags_dir: Path) -> None:
    src_dir = eu4_dir / "gfx" / "flags"
    flags_dir.mkdir(parents=True, exist_ok=True)
    tga_files = sorted(src_dir.glob("*.tga"))
    exported = 0
    for tga in tga_files:
        tag = tga.stem.upper()
        with Image.open(tga) as im:
            im = im.convert("RGBA")
            if im.size != (256, 256):
                im = im.resize((256, 256), Image.Resampling.LANCZOS)
            im.save(flags_dir / f"{tag}.png", "PNG")
            exported += 1
    print(f"exported {exported} flags to {flags_dir}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--eu4-dir", type=Path, default=DEFAULT_EU4_DIR)
    parser.add_argument("--out-dir", type=Path, default=DEFAULT_OUT_DIR)
    parser.add_argument("--flags", action="store_true")
    parser.add_argument("--flags-dir", type=Path, default=DEFAULT_FLAGS_DIR)
    args = parser.parse_args()

    export_dev_portal_assets(args.eu4_dir, args.out_dir)
    if args.flags:
        export_flags(args.eu4_dir, args.flags_dir)


if __name__ == "__main__":
    main()
