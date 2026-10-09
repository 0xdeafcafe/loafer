# Loafer notifier icon

`Loafer.icon` is the editable Icon Composer source for the macOS 26+ notifier (`~/.loafer/loafer.app`, `com.github.0xdeafcafe.loafer.notifier`). It follows sibling yap's composition and build recipe. The subject is a glossy aubergine penny loafer, side on with its toe to the right, a gold penny and four small Slack-coloured accent stitches. No text or Slack logo.

## Composition and appearances

- `Loafer.icon/icon.json`: native background and one coherent glass foreground, combined lighting, specular enabled, neutral shadow at 0.35.
- `Loafer.icon/Assets/loafer.png`: 1024 × 1024 RGBA shoe extracted with image generation; no tile or exterior cast shadow. Scale 0.9 and translation `[0, -24]` optically center the wide shoe.
- Default: cream/sand gradient `#FFF8E3` to `#EDD3A5`. Dark: plum/charcoal `#34252E` to `#151014`, retaining leather colour.
- Clear and Tinted use the automatic mono specialization. Translucency is disabled for Default/Dark and 0.18 for mono; the system supplies the tint.
- Use only specialization arrays, including unqualified defaults, as in yap. The system supplies the modern rounded mask, lighting and exterior spacing.
- `icon-1024.png`: selected full generated artwork, tile included. `AppIcon.icns`: legacy fallback from this artwork, with 16/32/128/256/512 sizes at 1×/2×.
- `Assets.car`: compiled native icon. `partial-info.plist`: compiler output, `CFBundleIconName=Loafer`.

## Rebuild

Select full Xcode 26+ with `xcode-select` or `DEVELOPER_DIR`. From the repository root, with a writable session `TMPDIR`:

```sh
sh internal/notify/icon/rebuild.sh
python3 internal/notify/icon/render-previews.py
assetutil --info internal/notify/icon/Assets.car
```

The build uses `xcrun actool` with `--app-icon Loafer --platform macosx --target-device mac --minimum-deployment-target 26.0`, checks the partial plist, then creates the fallback with `sips` and `iconutil`. Intermediates stay in `$TMPDIR`.

When integrating, copy `Assets.car` and `AppIcon.icns` into the notifier's `Contents/Resources`; set `CFBundleIconName=Loafer` and `CFBundleIconFile=AppIcon`. Do not merge the compiler's `CFBundleIconFile=Loafer` over the separately named fallback. No notifier code or installed bundle was changed here.

## Preview and verification

The preview script is adapted from yap, requires Pillow and Xcode 27's `ictool`, and writes only to a new directory in `$TMPDIR`. `ICTOOL` can override the renderer. It exports Default, Dark, ClearLight, ClearDark, TintedLight and TintedDark at 1024/128/32/16 px for design generations 26 and 27, plus a proof sheet. It uses native rendering, not simulated glass.

Verified with Xcode 27.0 (27A5228h): compilation succeeded; the partial plist names Loafer; `assetutil` reports three Loafer IconImageStack records (Aqua, DarkAqua, Tintable), each with two layers. All 48 renders had the expected dimensions and were visually inspected, including actual-size small renders and enlarged pixel views. The collar dip, raised strap, right-facing toe and heel remain readable; accent stitches and fine grain disappear at 16 px. Tinted Dark is subdued and is the weakest small-size variant. The legacy full artwork still has stray generated pixels around its outer tile edge despite a cleanup pass; these are absent from the native system tile. This is a remaining fallback quality limitation.

macOS 26 was checked through `--design-generation 26` on the available host, not a separate macOS 26 notification runtime. No app launch, notification delivery, installation, signing or commit was performed. Prompts and selected passes are in `PROMPTS.md`.
