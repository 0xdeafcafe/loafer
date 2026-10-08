# loafer logo options

Seven ways to draw the loafer and eight ways to animate it, for choosing one of each. They're all live in `tools/logo`, a stdlib-only Go program with its own module:

```
cd tools/logo
go run .                       # gallery: every variant on dark, light and aubergine
go run . -list                 # variants and animations, one line each
go run . -v 2                  # variant 2 in a mock loafer header
go run . -v 2 -a tap           # that header playing tap three times
go run . -v 2 -a all           # every animation in turn, labelled
go run . -v 2 -a lace -frames  # every frame of lace, laid out still
```

`-frames` isn't part of the brief. I added it because it's how this document's frame strips were made, and it's the quickest way to compare animations side by side.

## How they're drawn

- **Pixel art, packed into cells.** Each variant is pixel art, one letter a pixel, packed into half blocks (1×2 pixels a cell), quadrants (2×2), sextants (2×3), braille (2×4) or box-drawing glyphs (one a cell). Animations move and recolour the pixels, then the frame is packed again. That's how one animation works on every variant.
- **Two colours a cell.** A quadrant or sextant cell can show only a foreground and a background. Where a cell needs three, the packer keeps the pair that loses least. The sextant and quadrant art is drawn so that no still cell needs three colours.
- **Colours held off the ground.** The Slack colours are pushed away from the ground until they reach 3:1, the same way rush's `theme.Accent` does. On the light ground, yellow, green and blue darken a little; red is already there. Aubergine is pushed to 2:1, so on the dark ground it's lifted to a muted plum, about `653866`. The program emits truecolour only.
- **Three grounds in the gallery.** Dark `17,16,14` and light `250,249,245` are the two you asked for. I added aubergine `4A154B` as well, because rule 2a of `ui.md` puts the header on the workspace colour, which for LangWatch is aubergine. Any colour that's aubergine on the other grounds has to lift a long way there.
- **Size.** Every variant is 4 rows or fewer and 13 to 21 columns, including a spare column either side for dust and ticks. In the header, the text sits on rows 1 and 2, beside the middle of the shoe.

## Variants

### 1 · bands

Half blocks, 18×8 pixels. This is the `ui.md` spec, redrawn.

```
       ▄▄▄
 ▄██▄▄▀▀▀▀▀▀▄▄▄
 ████████████████▄▄
 ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```

- **Colours:**
  - The upper is in four hard vertical bands, red, yellow, green and blue, from heel to toe.
  - The strap is the band colour taken 45% toward black.
  - The penny is cream `FAF4E4`. It isn't gold because gold vanished against the yellow band.
  - The sole and heel are aubergine.
- **Strengths:**
  - It's the clearest silhouette: low heel counter, a shallow collar dip, the throat, then a long slope to a low toe.
  - It's unmistakably Slack-coloured.
  - Half blocks render the same in every terminal.
  - Every animation moves cleanly.
- **Weaknesses:**
  - It's loud. Four saturated bands sit next to the header's yellow `@ 3 mentions` and green `● live` and compete with them, which goes against rule 2 (colour is state).
  - The hard band edges read a little like a sneaker.
- **16 colours:**
  - The bands become red, yellow, cyan, cyan: green and blue merge.
  - The strap goes grey and the penny white.
  - The sole goes grey, or black if aubergine isn't lifted, which disappears on a dark terminal.
  - It still reads as a shoe with three bands.

### 2 · slack penny

Half blocks, 18×8 pixels. The playful one.

```
       ▄▄▄
 ▄██▄▄██▀▀███▄▄
 ████████████████▄▄
 ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```

- **Colours:**
  - The leather is aubergine, lifted to 2.2:1.
  - The strap is a slightly lighter plum.
  - The penny is Slack's mark in 2×2 pixels: blue at top left, green at top right, red at bottom left, yellow at bottom right, as in the logo.
  - The sole is a neutral grey taken from the ground's text colour.
- **Strengths:**
  - It reads as a burgundy loafer and as Slack at once.
  - All four brand colours are kept to the penny, so the header stays calm and the state colours stay loudest.
  - Its flip is a spin of the Slack mark.
  - It shares half blocks' portability and clean motion.
- **Weaknesses:**
  - On the dark ground the plum leather is quiet, about 2:1.
  - On the aubergine header ground, the leather has to lift to a pinkish plum to show at all.
  - The penny is only 2 cells, so at a glance it's a dot of colour, not the mark.
- **16 colours:**
  - The leather and the sole both become grey, so the sole line is lost, but the silhouette survives.
  - The penny becomes cyan, cyan, red and yellow.

### 3 · gradient

Quadrants, 36×8 pixels.

```
      ▗▄▄▄▖
 ▟▌▌▄▖▀▀▀▀▀▌▙▄▄
 ▌▌▌▌▌▌▌▌▌▌▌▌▌▌▌▌▄▄
 ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```

The `▌` cells are two neighbouring gradient colours, so in colour it's one smooth sweep.

- **Colours:**
  - One upper, blended red, orange, yellow, olive, green, teal, blue from heel to toe.
  - An unbroken aubergine strap, with a pale gold penny `FAD678` set into it from above.
  - An aubergine sole.
- **Strengths:**
  - It's the prettiest in truecolour.
  - Quadrants double the horizontal resolution, so the colour flows smoothly.
  - Cycle animates it beautifully.
- **Weaknesses:**
  - The olive in the middle is muddy, especially on the light ground.
  - Each cell is two colours already, so moves that shift by half a cell (tap, step, hop, shake) re-quantise it and the colours shimmer.
  - The plain-text copy is meaningless without colour.
- **16 colours:** it's the worst. Orange and olive both fall to grey, so you get red, grey, yellow, grey, cyan stripes.

### 4 · smooth

Sextants, 36×12 pixels.

```
        🬞🬭🬭
 🬻██🬹🬭🬭🬵🬎🬎🬎🬄🬹🬭🬏
 ███████████████🬹🬱🬭
 ████🬂🬂🬂🬂🬂🬂🬂🬂🬂🬂🬂🬂🬂🬂
```

- **Colours:** in parts:
  - green quarter (heel counter and collar)
  - blue vamp
  - red strap
  - yellow penny
  - aubergine sole and heel
- **Strengths:**
  - It's the finest solid profile: the rounded heel counter and the long toe slope are smooth.
  - The two-tone reads as separate leather pieces.
- **Weaknesses:**
  - Sextants (U+1FB00) look right in terminals that draw block glyphs themselves, Ghostty, kitty, WezTerm and foot among them. Elsewhere they depend on the font: Menlo has none, so Terminal.app may show boxes.
  - With five colours in small parts, any move by part of a cell (tap, step, hop) puts three colours in a cell and the strap smears for a frame or two.
  - It looks a little like a toy or a bowling shoe.
- **16 colours:**
  - Green and blue both become cyan, so the two-tone is lost.
  - The strap stays red and the penny yellow.
  - The sole goes grey.

### 5 · line

Box drawing, 3 rows.

```

 ╭──╮    ╭──────╮
 │  ╰────╯ ═●═  ╰──╮
 ╰█▄▄━━━━━━━━━━━━━━╯
```

- **Colours:**
  - red strokes for the heel counter and collar
  - blue strokes for the vamp
  - a yellow strap and penny `═●═`
  - a green heavy sole with a block heel
- **Strengths:**
  - It's the lightest and calmest, rush-like: geometric glyphs, no fill.
  - It works in any font, and copies and pastes as text.
  - It degrades best of the coloured ones.
  - Because it's 3 rows, hop can lift it a whole row with nothing clipped.
- **Weaknesses:**
  - It reads as a low slip-on with a strap, but less instantly as a loafer than the filled ones. At 4 rows, box drawing can only step, not slope.
  - The empty top row looks unbalanced beside a 4-row header.
  - A line can't bend by part of a row, so tap and step don't bend it: tap lights the toe instead, and step only slides.
- **16 colours:** red, cyan, yellow and cyan strokes. They're thin lines in base colours, so nothing merges into a blob.

### 6 · braille

Braille, 38×16 dots.

```
  ⣀⡀    ⡠⠤⠤⢄⡀
 ⢸ ⠈⠑⠢⠤⠊⠘⠛⠛⠃⠈⠉⠒⠦⣀
 ⢸               ⠉⠢⡀
 ⢸⣿⣿⡟⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠓
```

- **Colours:**
  - a dotted outline graded red to blue, one colour a cell
  - the strap filled, with a gold penny
  - a solid aubergine sole and heel
- **Strengths:**
  - It's the highest resolution and the most elegant: the collar curve, the throat and a long, gentle toe slope are all visible.
  - Every animation is clean on it, because a braille cell has only one colour.
- **Weaknesses:**
  - It's faint, because dots don't fill the cell.
  - Fonts draw braille at different sizes and spacings.
  - It's hard to see at a glance in the corner of a busy screen.
  - The sole's dot texture can look like a pattern rather than a sole.
- **16 colours:** the outline steps through red, grey, yellow, grey and cyan, and the sole goes grey. It reads by shape alone.

### 7 · mini

Sextants, 2 rows, 22×6 pixels in use.

```

 🬵🬱🬭🬻🬎🬹🬭🬏
 ██🬎🬎🬎🬬███🬺🬃

```

- **Colours:** one red silhouette, with the heel block and the arch cut out of its bottom edge, and a yellow penny.
- **Strengths:**
  - It's the simplest mark. It sits exactly beside the two text rows, and at 13 columns it's narrow.
  - It reads as a shoe even this small.
  - It has only two colours, so it never smears when it moves.
  - Cycle runs it through all four brand colours.
  - It would make a good fallback for the narrow header (60 to 100 columns), where `ui.md` currently drops the logo.
- **Weaknesses:**
  - It's mostly one colour, so it's the least "Slack".
  - It has the same sextant font dependency as 4.
  - The strap is implied only by the penny.
- **16 colours:** red with a yellow dot. It keeps all its meaning.

### 16-colour reference

These are the nearest colours in xterm's default 16-colour palette. Themes remap these 16, so treat them as a guide.

| colour | becomes |
|---|---|
| red `E01E5A` | red (1) |
| yellow `ECB22E` | yellow (3) |
| green `2EB67D` | cyan (6), close to grey (8) |
| blue `36C5F0` | cyan (6) |
| aubergine `4A154B` | black (0), gone on a dark terminal |
| lifted aubergine `653866`, plum leather | bright black (8) |
| gold `FAD678`, cream `FAF4E4` pennies | white (7) |
| copper `C86E3C` (flip's back face) | bright black (8) |
| gradient mixes: orange, olive | bright black (8) |
| gradient mix: teal | cyan (6) |

In short: green and blue collapse into one cyan, aubergine needs its lift or it vanishes, and in-between mixes turn grey. A 16-colour fallback should use solid parts in red, yellow and cyan, which favours 5 and 7.

## Animations

All are for events, never idle loops. Each is at most 1.1 s, at 50 to 70 ms a frame, and ends on the still logo.

| | event | frames | length | clean on | caveats |
|---|---|---|---|---|---|
| tap | a mention arrives | 10 × 60 ms | 600 ms | 1 2 6 7 | 5: lights the toe, no bend. 3, 4: a little smear |
| glint | your message is sent | 14 × 60 ms | 840 ms | all | |
| cycle | reconnected | 16 × 60 ms | 960 ms | all | best on 1 3 6 7; on 2 only the penny turns |
| step | sending a message | 14 × 60 ms | 840 ms | 1 2 6 7 | 5: slides only. 3, 4: smear |
| hop | startup, once loaded | 13 × 60 ms | 780 ms | 1 2 5 6 7 | 3: slight shimmer. 4 smears |
| flip | a mention, quieter | 12 × 70 ms | 840 ms | all | 2: the four-colour mark spins. 5: the glyph turns |
| lace | startup | 18 × 60 ms | 1080 ms | all | |
| shake | send failed, signed out | 10 × 50 ms | 500 ms | all | 3, 4: the half-cell shake shimmers slightly |

### tap

The toe lifts half a cell and taps down, twice. The heel and the throat stay planted, with the bend at the ball of the foot. Each landing leaves a yellow `·` and `˙` off the toe that fade over 2 frames; yellow because yellow means "needs you".

```
0 · 0ms                1 · 60ms               4 · 240ms              5 · 300ms
       ▄▄▄                    ▄▄▄                    ▄▄▄                    ▄▄▄
 ▄██▄▄▀▀▀▀▀▀▄▄▄         ▄██▄▄▀▀▀▀▀▀▄▄█▄▄       ▄██▄▄▀▀▀▀▀▀▄▄▄         ▄██▄▄▀▀▀▀▀▀▄▄█▄▄
 ████████████████▄▄     █████████████▀▀▀▀▀     ████████████████▄▄˙    █████████████▀▀▀▀▀˙
 ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀     ████▀▀▀▀▀▀▀▀▀          ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀·    ████▀▀▀▀▀▀▀▀▀     ·
```

| frames | toe |
|---|---|
| 0 | down |
| 1 to 3 | up |
| 4 | down, ticks |
| 5 to 7 | up |
| 8 | down, ticks |
| 9 | ticks fade |

`ui.md` already names this as the mention reaction. It's the one most likely to be noticed from the corner of your eye, because the silhouette itself moves.

### glint

A diagonal band of white light, slanted `/` and about 3 cells wide, sweeps across the leather from before the heel to past the toe, mixing up to 80% toward white at its centre. The sole and heel aren't lit. It moves about 1.8 cells a frame, so it takes 14 frames to cross. Only colours change, so it works identically on every variant. It's rush's shimmer, borrowed.

### cycle

The four brand colours roll once along the shoe, heel to toe, with an ease in and out. Each colour blends through its neighbours on the way, so red passes through purple to blue, blue through teal to green, and so on. At frame 15 every colour is back where it started.

- On 1, the bands march toward the toe.
- On 3 and 6, the gradient slides.
- On 7, the whole shoe goes red, purple, blue, teal, green, yellow, orange, red.
- On 4 and 5, the parts swap colours.
- On 2, only the penny moves.

It suits "reconnected": everything's flowing again.

### step

The heel lifts (the back bends up about the ball of the foot) and the shoe moves a cell forward. Then the heel plants with a puff of dust behind it, it holds, and it slides home.

```
0 · 0ms                2 · 120ms              4 · 240ms              7 · 420ms
       ▄▄▄               ▄▄  ▄▄▄▄                ▄▄  ▄▄▄▄                    ▄▄▄
 ▄██▄▄▀▀▀▀▀▀▄▄▄         ████▀▀▀▀▀▀▀▄▄▄          ████▀▀▀▀▀▀▀▄▄▄         ▄██▄▄▀▀▀▀▀▀▄▄▄
 ████████████████▄▄     ▀▀▀▀▀▀██████████▄▄      ▀▀▀▀▀▀██████████▄▄   ˙ ████████████████▄▄
 ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀     ▀▀▀▀  ▀▀▀▀▀▀▀▀▀▀▀▀      ▀▀▀▀  ▀▀▀▀▀▀▀▀▀▀▀▀   · ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```

| frames | what happens |
|---|---|
| 0 to 2 | heel up |
| 3 to 4 | forward one cell |
| 6 | heel down, puff |
| 6 to 9 | hold |
| 10 to 12 | slide back |

It suits sending: the message "goes".

### hop

A small hop: up half a cell with the toe tilted up, down, then a puff of dust each side (`·` `∙` `·` `˙` `˙`, fading).

```
0 · 0ms                2 · 120ms              6 · 360ms              7 · 420ms
       ▄▄▄              ▄▄  ▄███▀▀▄▄▄                ▄▄▄                    ▄▄▄
 ▄██▄▄▀▀▀▀▀▀▄▄▄        ████▀▀▀▀▀███████▄▄      ▄██▄▄▀▀▀▀▀▀▄▄▄         ▄██▄▄▀▀▀▀▀▀▄▄▄
 ████████████████▄▄    ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀      ████████████████▄▄     ████████████████▄▄
 ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀    ▀▀▀▀                   ·████▀▀▀▀▀▀▀▀▀▀▀▀▀▀·   ∙████▀▀▀▀▀▀▀▀▀▀▀▀▀▀∙
```

| frames | what happens |
|---|---|
| 1 to 5 | in the air, the toe tilted up at 2 to 3 |
| 6 | landing |
| 6 to 11 | dust |

It's a cheerful "ready" once startup has loaded, or for "all caught up".

### flip

The penny flips in its slot: face up, edge on (one pixel, pale), the copper back, edge on, face up. It's a whole turn in 840 ms.

- On 2, the four-colour mark turns a quarter each frame, so the Slack mark spins.
- On 5, the penny turns by glyph:

```
 │  ╰────╯ ═●═  ╰──╮     │  ╰────╯ ═◖═  ╰──╮     │  ╰────╯ ═▮═  ╰──╮     │  ╰────╯ ═◗═  ╰──╮
```

It's the quiet mention: a DM or thread reply rather than an `@you`. On the other variants the penny is only 2 pixels, so it's easy to miss.

### lace

The startup draw-in, about 1.1 s:

1. The sole and heel run from heel to toe (frames 0 to 6).
2. The upper sweeps in after it, on a slant, with a bright leading edge like a stitch being pulled through (frames 3 to 12).
3. The strap appears (frame 12).
4. The penny appears with a `✦`, then `·`, then `˙` above it (frames 14 to 16).

```
1 · 60ms               4 · 240ms              7 · 420ms              10 · 600ms             14 · 840ms
                                                                            ▄▄▄                    ▄▄▄
                        ▄                      ▄██▄ ▀                 ▄██▄ ▀▀▀▀▀▀▄           ▄██▄▄▀▀▀▀▀▀▄▄▄
                        █▄                     ███████▄               █████████████          ████████████████▄▄
 ███                    ████▀▀▀▀▀▀▀▀           ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀     ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀     ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```

It plays once, when the app opens.

### shake

A quick shake: half a cell right, left, right, left, right, left, a quarter, then still. The shoe washes red, 60% fading to nothing by frame 8.

- On half blocks and box drawing the step is a whole column.
- On the 2-pixel-wide kinds it's half a column, which re-quantises 3 and 4 slightly.

It's for errors: a send that failed, being signed out.

## Recommendation

**Variant 2 (slack penny), lace at startup, and tap for a mention.**

- **It reads as both things at once.** Within a glance it's a loafer, from the low slip-on line, the strap across the throat and the low heel. And it's Slack: the aubergine leather is Slack's own sidebar colour, and the penny is the four-colour mark.
- **It follows the visual language.** Rule 2 says colour is state. Variant 2 keeps the brand colours to a 2×2-pixel penny and leaves the shoe a quiet plum, so the header's yellow mentions and green "live" stay the loudest colours on the row. Variant 1's four full bands compete with them.
- **It's built like rush's bottle.** It's a 4-row half-block pixel object, so it renders identically in every terminal (no sextant or braille font dependency). Every animation moves cleanly on it, because half a cell is one whole pixel and nothing is re-quantised.
- **Lace ends on the brand.** The sole, then the upper and strap draw in, and the Slack mark arrives last with a `✦`.
- **Tap is the mention reaction `ui.md` already specifies, and it works here.** It moves the silhouette, so you notice it without looking. It's 600 ms with quiet yellow ticks.

For the other events with variant 2: glint when a message is sent, cycle on reconnect (here it spins the penny's colours), shake on a failed send. Flip, the spinning mark, is a good quieter reaction for DMs and thread replies.

**Caveats:**

- **The aubergine header ground.** If the header really sits on the workspace colour (rule 2a), the leather has to lift to a lighter plum to show; the gallery's third column shows it. It still reads, but with less contrast. If that matters more than calm, choose variant 1 (bands): it has the same shape and motion, and shows on any ground.
- **16 colours.** The leather and sole both fall to grey. The silhouette survives; the sole line doesn't.
- **Narrow headers.** Consider variant 7 (mini) for 60 to 100 columns rather than dropping the logo.
- **`ui.md` is unchanged.** Its Logo section still describes variant 1's bands. It would need updating if variant 2 is chosen.
