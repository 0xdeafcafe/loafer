# Loafer notifier icon

An illustrated penny loafer with a low throat, shorter vamp, fuller rounded toe with slight spring, tonal moc-toe apron, solid slotted strap and separate stacked heel. Eight hand-drawn SVG layers form four depth groups. The notifier identity is `com.github.0xdeafcafe.loafer.notifier`.

The selected direction is a warm tan shoe on a saturated native aubergine gradient, `#611F69` to `#3F0E40`. In the preceding design pass we also rendered the same larger silhouette in aubergine on a light `#F0E6DD` tile; the saturated direction had stronger presence and clearer shoe-to-background separation at 32 px. That comparison selected the background retained here. The alternative source stays in scratch, not in the delivered document.

## Layers and effects

The shoe spans approximately 87% of the 1024-square canvas width. Shared scale 1 and translation `[0, -35]` center its silhouette; the heel finishes below the sole, leaving a visible arch. No bulky outsole, outline strokes, textures or painted effects are used. Foreground SVGs contain only opaque flat filled paths (plus geometric transforms). Background gradients and all rendered lighting are native.

Back to front; JSON stores groups front to back:

| Group | SVG layers | Default/Dark translucency | Neutral shadow |
| --- | --- | --- | --- |
| Sole and stacked heel | `heel.svg`, `sole.svg` | Disabled | 0.12 |
| Upper and opening | `upper.svg`, `lining.svg` | Disabled | 0.08 |
| Moc-toe apron | `apron.svg` | 0.04 | 0.06 |
| Penny strap | `strap.svg`, `slot.svg`, `penny.svg` | 0.06 | 0.10 |

All groups use combined lighting. Foreground specular is deliberately disabled to avoid puffy outlined edges. Glass is enabled only on the apron and strap; the other six layers preserve crisp matte silhouettes. Depth comes from overlapping planes, restrained translucency and native shadows. The inset apron edge suggests the moc-toe seam through a change of tone, not a stroke.

Main tones: upper `#D7BA94`, apron `#EAD2AE`, strap `#C5A47B`, lining `#987459`, sole `#735039`, heel `#5A3D31` with one filled stack division. The dark slot `#63422D` contains a larger rounded inset dominated by Slack blue `#36C5F0`, green `#2EB67D`, yellow `#ECB22E`, and red `#E01E5A` form three smaller end pieces. This is a compact colour detail, not a rainbow band or the Slack logo.

Dark keeps the tan shoe on a deeper native gradient (`#421848` to `#200A27`). Clear/Tinted share an automatic background and explicit grey foreground fills: upper `#BCBCBC`, apron `#DADADA`, strap `#B0B0B0`, slot `#353535`, lining/sole `#555555`, heel `#404040`. Each group has 0.06 mono translucency. Geometry is identical in every appearance; the system chooses the tint.

## What we took from the references

- **Tower:** a strong outline, saturated tile and distinct overlapping parts carry the object. Installed Tower 16.0 has a background plus four foreground groups. We used that restraint and separation, without borrowing its artwork.
- **Amphetamine:** its bundled pill-only `AmphIcon_v1` is recognisable through a small number of deliberate shapes. Installed 5.3.2 uses a pill-and-screen icon and has no native glass stack; the comparison shows that actual installed icon.
- **Paw:** the [official fox construction graphic](https://cdn-content.paw.cloud/versions/releases/paw-3.2.1-release/paw-3.2.1-logo.png), linked from its [release history](https://paw.cloud/updates), shows the value of confident tapering curves. We took geometric economy, not its painted highlights.
- **Apple:** installed Notes has two foreground groups; Podcasts and Home each have four, with vector foregrounds. We kept separate planes but reduced glass edging. At notification size, silhouette and broad light/dark contrast do the work; the small penny colours are a secondary detail.

Reference catalogs were inspected with `assetutil --info`, and artwork through IconServices/ICNS extraction. Installed Apple apps were examined on macOS 27. See [Apple's App icons guidance](https://developer.apple.com/design/human-interface-guidelines/app-icons). No reference art is included in the source.

## Files and rebuild

- `Loafer.icon/`: JSON and eight SVGs; no raster foreground.
- `Assets.car`, `partial-info.plist`: compiled catalog and compiler metadata.
- `icon-1024.png`: native Default export, design generation 26.
- `AppIcon.icns`: legacy fallback from that export, 16/32/128/256/512 at 1×/2×.
- `rebuild.sh`, `render-previews.py`, `PROMPTS.md`: build, native previews and provenance.

Select full Xcode using `xcode-select` or `DEVELOPER_DIR`. The workflow uses Xcode 27's `ictool` (`ICTOOL` can override it); previews require Pillow. With writable session `TMPDIR`, from the repository root:

```sh
sh internal/notify/icon/rebuild.sh
python3 internal/notify/icon/render-previews.py
assetutil --info internal/notify/icon/Assets.car
```

`rebuild.sh` passes absolute source paths to the Xcode tools and runs `xcrun actool` with `--app-icon Loafer --platform macosx --target-device mac --minimum-deployment-target 26.0`, validates `CFBundleIconName=Loafer`, renders the master with `ictool`, and builds ICNS with `sips`/`iconutil`. Intermediates stay under `$TMPDIR`.

Integration remains `CFBundleIconName=Loafer`, `CFBundleIconFile=AppIcon`, with the catalog and fallback in `Contents/Resources`. Do not merge the compiler's `CFBundleIconFile=Loafer` over the fallback name. The installed notifier was not modified.

## Verification

`rebuild.sh` passed with Xcode 27.0 (27A5228h) on macOS 27.0. `assetutil` confirms three appearance stacks, each with five planes (background plus four groups), retaining all eight Vector assets. The master PNG matches the final 1024 Default export.

All 48 native renders were dimension-checked and visually inspected: Default, Dark, ClearLight, ClearDark, TintedLight, TintedDark at 1024/128/32/16 px, generations 26 and 27. The silhouette separates in Dark and mono. A larger equal-quarter penny and a blue-dominant inset were compared at 16/32 px. The latter was selected: at 16 px it becomes one subtle clean blue accent, not four individually resolved colours. At 32 px the blue remains dominant with a warmer end detail. The slim profile is intentional; it uses width rather than a tall, cartoon-like upper. Generation 26 is renderer emulation, not a separate macOS 26 notification test.

This pass retains the background, palette, heel, eight layers and all material settings from `e89d873`. The forefoot is fuller and rounder, and the strap moves forward to shorten the visible vamp while preserving approximately 87% width. The slightly raised toe and slim sole avoid a pointed slipper silhouette.

This pass's previews: `$TMPDIR/loafer-icon-previews-wsgu9bs_/` (`glass-check.png`, `small-26.png`, `small-27.png`, `large-26.png`, `large-27.png`, `previews/`). The comparison is `$TMPDIR/loafer-toe/old-vs-new-128-32-16.png`: the exact `e89d873` source versus this revision, rendered with the same generation-26 renderer at actual 128/32/16 px. No installation, notification delivery or commit was performed.
