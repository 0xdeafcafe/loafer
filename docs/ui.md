# loafer UI

Slack's layout and features, drawn in rush's visual language. If a Slack element and a rush idiom disagree, keep Slack's *structure* (what's where, what it means) and rush's *rendering* (glyphs, greys, fills, spacing, hints). The rush catalogue this is based on: `rush/internal/ui/style.go`, `convo/style.go`, `ui/cmdbar.go`, `ui/dialog.go`, `ui/inputbox.go`; the Slack reference is the desktop app (screenshots of 2026-10-08).

## Rules

1. **One ground, four greys, one accent.** rush's palette and theme mapping as is (`theme.Ground`, `Ink/Surface/Accent`, colour-blind mode). Text 226,221,211; sub 168,162,152; dim 122,117,108; faint 72,68,63. Accent is rush's terracotta 217,119,87.
2. **Colour is state, never decoration.**
   - orange: focus, you, selection rail, matched characters
   - yellow: needs you, meaning mentions, @you, overdue
   - blue: links, #channels and @others
   - green: ok, primary buttons, "recovered"
   - red: failure, danger buttons, delete
   - lavender: drafts and scheduled
   - Slack's attachment colours are an exception: they're content, drawn as a narrow `▌` bar mapped through `theme.Accent` so they stay readable on any ground.
2a. **Workspace colour.** The workspace's sidebar theme colour (aubergine for LangWatch) is the ground of the header, the tab row and the sidebar pane; the message, thread and overlay panes keep rush's ground. The header also gets rush's static gradient wash toward the right, ramping into the same colour. All inks on the workspace ground go through `theme.Ground{BG: workspace}` so text keeps 4.5:1 and dim keeps 3:1. The selection fill on the sidebar is the workspace colour lifted one step (as Slack's selected row), still with the orange `▍`. Fallback when a workspace has no theme: rush's `panelBG`.
3. **Bold is for what wants you**: unread channel names, mention counts, author names, section titles, key names in hints. Read things are plain; muted things are faint.
4. **No borders around content.** Hierarchy comes from greys, one-row gaps and fills. Rounded boxes only for things you type into and things floating over the screen: the composer, ctrl+k, modals and menus.
5. **Every list row has a marker column** and the same selection: a `selBG` fill plus an orange `▍` in column 0. Hover is `hoverBG`.
6. **Hint line grammar**: `key label · key label`. The key is bold, the label dim, and the separator a faint ` · `. At most 5 pairs, and the least important drop first when narrow.
7. **One blank row** between groups, never two. One level of tabs per pane.
8. **Emoji only in content** (messages, reactions, statuses, section emoji the user chose). The chrome uses rush's geometric glyphs.
9. **Animate only real work**: `✻` spinner after a button click or while sending, at rush's slow tick. Nothing moves when idle.

## Glyphs

| Meaning | Glyph | Colour |
|---|---|---|
| public channel | `#` | dim (bright when unread) |
| private channel | `⊡` | dim |
| DM, active / away | `●` / `○` | green / faint |
| group DM | `⁂` | dim |
| app / bot | `◇` | dim |
| mention / thread reply / DM / reaction / app (activity) | `@` `↩` `●` `☺` `◇` | yellow / sub / sub / sub / dim |
| draft | `✎` | lavender |
| saved for later | `◆` | orange |
| fold | `▾` `▸` | faint |
| selection rail | `▍` | orange |
| attachment bar | `▌` | attachment colour |
| quote / forwarded message rail | `▏` | faint |
| sending / working | `✻` (rush spinner family) | orange |
| sent ok / failed | (nothing) / `✗ retry` | - / red |
| new-messages line | `─── new` | orange |
| prompt | `❯` | orange |
| more | `⋯ 51 more replies` | blue |

## Logo

A loafer, side on with the toe to the right, in Slack's four brand colours. The app uses both looks and switches between them by state (below). Both are 4 rows tall, like rush's bottle, and the art and animations come from `tools/logo` (see [logo-options.md](logo-options.md); preview with `cd tools/logo && go run . -v 1` or `-v 6`).

**bands**: the loud one. Half blocks, so they render the same in every terminal.

```
       ▄▄▄
 ▄██▄▄▀▀▀▀▀▀▄▄▄
 ████████████████▄▄
 ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```

- **Upper.** Four vertical bands from heel to toe: red E01E5A, yellow ECB22E, green 2EB67D and blue 36C5F0.
- **Strap.** The band colour taken toward black.
- **Penny.** Cream FAF4E4.
- **Sole and heel.** Aubergine.

**braille**: the quiet one. Finer, but it depends on the font's braille.

```
  ⣀⡀    ⡠⠤⠤⢄⡀
 ⢸ ⠈⠑⠢⠤⠊⠘⠛⠛⠃⠈⠉⠒⠦⣀
 ⢸               ⠉⠢⡀
 ⢸⣿⣿⡟⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠓
```

- **Outline.** Shaded red to blue, one colour per cell.
- **Strap.** Filled.
- **Penny.** Gold.
- **Sole and heel.** Aubergine.

**Which one shows.** The logo is a status light: quiet when nothing needs you, loud when something does. It changes only when the state does, never on a timer.

| State | Logo |
|---|---|
| Starting | bands, playing `lace` (1.08 s), then settling into whichever state applies |
| All caught up, live | braille, in its colours |
| Something needs you: unread mentions, DMs, thread replies to you, or a rush agent waiting | bands. The switch plays `tap` (600 ms). Each new mention while it's already bands taps again. |
| You've read the last of them | back to braille, with no animation |
| Reconnecting or offline | braille, all in `dim` grey (no colour, because nothing is live) |
| Signed out, or the token stopped working | braille in red, after one `shake` (500 ms) |
| A send failed | `shake` on whichever shoe is showing, and nothing else changes |
| Do not disturb | braille, even with mentions waiting. The mention count still shows in the header. |

**How it works.** The UI computes the logo state from the store's counts and the connection state, the same values the header counts are drawn from. Each look's frames are rendered once at start and cached per ground, so switching only swaps cached rows. An animation runs on its own short timer while it plays, and the timer stops when it ends.

Both looks:
- **Colours.** All pass through `theme.Accent`, so they hold their contrast on light grounds. The aubergine is lifted to a muted plum (about 653866) on dark grounds, and further on the aubergine header.
- **Narrow terminals.** Under 100 columns the logo is dropped and only the wordmark shows.
- **Notifier icon.** The notifier app's icon is the bands shoe, rendered as a PNG.

## Main screen (Home, channel open, thread closed)

```
  loafer  LangWatch ▾    @ 3 mentions   12 unread   ✻ 1 agent waiting               ● live   alex
  [Home] DMs ·2  Activity ·3  Later ·5  Claude                    ctrl+k jump · ctrl+f search
 ─────────────────────────────────────────────────────────────────────────────────────────────────
  HOME                      │ # prod-alerts-page   someone must look now          ⊙ 4   ◆   ⌕
  ▾ ◇ Threads             2 │ messages  files  pins
    ✎ Drafts & sent      65 │──────────────────────────────────────────────────────────────────
                            │                              today ▾
  ▾ 🚨 big bong ──────────── │ ▌ What happened: 37 groups are blocked at once (page threshold
    # opensource-alerts     │ ▌ 5+). A single hand-unblockable group no longer pages - this
    # prod-alerts-attend    │ ▌ fires only when blocks pile up systemically…
    # prod-alerts-digest    │ ▌ ⋯ show more
 ▍  # prod-alerts-page      │ ▌ Grafana v12.4.3 · 20:49
    # prod-infra-reviews  1 │
    # prod-security         │ ▌ ✓ RECOVERED · Fatal Errors
                            │ ▌ Fatal Errors recovered
  ▸ Team  4 ───────────────  │ ▌ Fired 18:49 → 18:54 UTC. Review the window
                            │ ▌    alert=Fatal Errors
  ▾ good-boys ────────────── │ ▌    status=resolved
    ⊡ agent-kanban-keeper   │ ▌    started=2026-10-08T18:49:10Z
    ⊡ agent-box-status      │ ▌ Grafana v12.4.3 · 20:54
    ⊡ agent-support       2 │──────────────────────────────────────────────────────── new ──
    ⊡ agent-experiments     │ ██ Grafana Alerts  app  21:49
                            │ ██ ▌ 🚨 CRITICAL · Blocked Groups Growing
  ▾ Engineering ──────────── │    ▌ What happened: 37 groups are blocked at once…
    # dev                 @2 │    ▌ Next steps:
    # github-issues         │    ▌ 1. Open https://app.langwatch.ai/ops - are the blocks…
                            │    ▌ ⋯ show more
  ▸ Social · Product · 3 more│    ▌ Grafana v12.4.3 · 21:49
  ↓ 4 more unread           │
                            │ ╭ to #prod-alerts-page ───────────── enter sends · ⇧enter line ╮
                            │ │ ❯ a message for #prod-alerts-page                             │
                            │ ╰ @ mention · : emoji · ctrl+u file ─────────────── ✎ draft ──╯
  ↑↓ move · enter open · t thread · . actions · ctrl+k jump
```

Notes:
- **Header**, two rows plus a rule. When the terminal is 100 columns or wider, the logo (below) sits to the left of those two rows, and the header grows to 4 rows, as rush's does. Mockups below omit it.
  - Row 1: the wordmark (bold italic `loafer`, as rush does), the workspace, the state counts in rush's header style, and connection state on the right.
  - Row 2: Slack's left rail as rush's top tabs. The active one is a filled chip; badges are `·N`, yellow when they're mentions.
  - The header, tab row and sidebar sit on the workspace colour (rule 2a). The header also gets rush's static gradient wash toward the right edge, ramping into that colour.
- **Sidebar** = rush's list pane.
  - Sections use rush's section rule `▾ name ──────`, with the section's emoji when the user gave it one. Collapsed sections with unreads show a count; quiet collapsed sections fold into one `▸ Social · Product · 3 more` line.
  - Rows: unread names are bright and bold, read ones plain sub, muted ones faint. On the right, the unread count is dim, and mentions are `@2` in yellow bold.
  - `↓ 4 more unread` and `↑ …` at the edges are Slack's "More unread messages" pills, drawn as rush's `↓ N more` chip.
- **Avatars**: 2×2 cells beside the name and first line. They're kitty images, or initials on a chip tinted from the user's id.
- **Pane header**: rush's chrome-filled block. It shows the channel, its topic dim, members `⊙ 4`, saved `◆`, and search `⌕`. One tab row, with the active tab underlined in orange.
- **Messages**:
  - **Header.** A 2×2-cell avatar (kitty image, or initials on a tinted chip), then the bold name, `app` as a dim chip, and the time dim. Consecutive messages from the same author within 5 minutes drop the header, and their time shows only when selected.
  - **Day dividers** are a dim label centred on nothing, Slack's pill without the pill. The current day sticks to the top as a chrome chip.
  - **The "new" line** is a full-width orange rule with `new` at the right.
  - **Attachments and blocks** sit behind a `▌` in their colour. Fields lay out in two columns when there's room. The footer is dim with ` · `. Long bodies fold at 8 rows to `⋯ show more`, in blue.
  - **Code blocks** are a `panelBG` fill, indented, highlighted with rush's highlighter. Inline code is cText on the chip fill.
  - **Selected message**: `selBG` fill plus the orange `▍`, like any row. Its actions are in the hint line and the `.` menu, rather than a hover toolbar.
- **Composer**: rush's input box exactly.
  - The rounded edge is orange when focused.
  - The edges carry labels: who it goes to on the top left and what enter does on the top right, hints on the bottom left, and draft state on the bottom right.
  - Inside are the `❯` and a faint placeholder.

## Thread open (side pane)

```
 ── channel (narrows) ──────────────────────│ thread  #crisp-chats                    esc close
                                            │──────────────────────────────────────────────────
 ██ LangWatch  app  16:37                   │ ██ LangWatch  app  16:37
 ██ 💬 Crisp conversation from Richard.     │ ██ 💬 Crisp conversation from Richard.
    ▌ ‼ Conversation is unresolved.         │    ▌ ‼ Conversation is unresolved. Please reply.
    ▌  Mark as resolved                    │    ▌  Mark as resolved 
    ↩ 4 replies · last 16:47  ◦◦            │
                                            │ 4 replies ─────────────────────────────────────
                                            │ ██ LangWatch  app  16:37
                                            │    Crisp conversation with Richard.
                                            │    ▌ Location            Device
                                            │    ▌ 🇮🇹 Palermo, Italy   Safari, macOS
                                            │    ▌ Email               Segments
                                            │    ▌ richard@getreg…     No segments
                                            │ ██ visitor58096  app  16:37
                                            │    Hi, we'd like to move from EU to US data
                                            │    residency if possible…
                                            │
                                            │ ╭ reply in thread ───────── ☐ also #crisp-chats ╮
                                            │ │ ❯ reply…                                      │
                                            │ ╰ ctrl+b also send to channel ──────────────────╯
```

- The thread is rush's side pane: rush's `sideWidth()` rule (28% of the width, at least 30 columns, at most 64), resizable by dragging the `│`. Below `minPane` it replaces the channel, and esc comes back.
- Thread summaries under a message: `↩ 4 replies · last 16:47` in blue, then the repliers' avatars as tiny 1-cell images, or their initials.
- The "N replies" divider is rush's section rule.

## Block Kit

```
 ██ Terrafied  app  21:12
    ▌ ⚙ Production Terraform Review Request
    ▌ langwatch/langwatch-saas · 9b57ad7 on main
    ▌ feat(agents-box): run the claude agents in rush hosts with account switching (#1293)
    ▌ rogeriochaves · Run #3531 · Plan: 1 to add, 3 to change, 1 to destroy
    ▌ ~ kubernetes_deployment.langwatch
    ▌ ~ kubernetes_deployment.langwatch_workers
    ▌ ± kubernetes_job_v1.lwql_access_render
    ▌  Review Terraform Plan    View on GitHub ↗  ✻
    ▌ 👍 1   👀 1   ☺+
```

Rendering:
- **Buttons** are one-row chips with one space of padding on each side:
  - primary: green fill
  - danger: red fill
  - default: chip fill with cText
  - link buttons: a trailing `↗`
- **Choosing a button**: in a selected message, `tab` / `shift+tab` move between its buttons, and the chosen one gets an orange underline. `enter` presses it, and `✻` shows until the response arrives.
- **Selects and overflow menus** are a chip with `▾` that opens rush's picker sheet.
- **Context** blocks are dim.
- **Dividers** are a faint rule inside the `▌`.
- **Images** use kitty placeholders up to 12 rows tall, or `▣ name 1200×800` when graphics are off.
- **Headers** are bold and bright, with a blank row above unless they come first. **Sections** are their text, then their fields, two columns when each gets 20 cells, then the accessory as its chip.
- A message with blocks shows the blocks and not its text, as Slack does: the text is the notification's fallback. If none of its blocks can be drawn, the text shows instead. One that can't (an `input`, a `table`) is a faint `unsupported block (input)`.
- **Legacy attachments**: the pretext above the bar, then inside it the author, the title (a link), the text, fields (short ones two to a row), an image, any blocks, and the footer with its time.
- **Files** are a line each: `▣` image, `▶` video, `♪` audio, `▤` the rest, then the name (a link to it in Slack), its type and size, dim. A deleted file is a faint `▤ this file was deleted`.

For now all of this is read-only: buttons and menus draw, but `tab` and `enter` don't press them yet.

Reactions: chips with the emoji and count. Yours have an orange count, others dim. `☺+` adds one. Custom emoji are 2×1-cell images, or `:name:` in dim.

Mentions inside text:
- `@Alex` (you) is bold yellow on the `askBG` fill.
- `@others` are blue.
- `#channel` is blue.
- Links are blue and underlined with OSC 8. A bare URL shows its host plus a shortened path.

### Modal

```
              ╭──────────────────────────────────────────────────────────────╮
              │  ██ Terraform Review                                ctrl+o ↗ │
              │                                                              │
              │  langwatch/infrastructure · run 3531                         │
              │ ┃ ! HIGH RISK - in-place changes to 2 Deployments, 1         │
              │ ┃   StatefulSet                                              │
              │  rogeriochaves · 9b57ad7 on main · run on GitHub ↗           │
              │  ● 1 to add   ● 3 to change   ● 1 to destroy                 │
              │                                                              │
              │  What changes ──────────────────────────────────────────     │
              │  -/+ kubernetes_job_v1.lwql_access_render - args [] → null   │
              │  ~ 2 Deployments - image …langwatch-app:git-c400943 → …      │
              │                                                              │
              │  By module ─────────────────────────────────────────────     │
              │  root module                                     Inspect   │
              │  1 replaced · 2 changed                                      │
              │  module.clickhouse                               Inspect   │
              │                                                              │
              │ ┃ ! You're about to affect production infrastructure.        │
              │                                                              │
              │                        esc cancel     Submit Decision      │
              ╰──────────────────────────────────────────────────────────────╯
```

- The modal is rush's sheet: `edgedBox`, a `panelBG` fill, the background faded, and its width at most 72 columns. It scrolls inside, and the footer stays pinned.
- Callouts get a full-width fill and a `┃` rail:
  - danger: `errBG` with a red `!`
  - warning: `askBG` with a yellow `!`
- Inputs are rush input boxes inside the sheet.
- Keys: `tab` moves focus, and `ctrl+enter` submits.
- Leaving with edits opens rush's yellow-edged confirm: `Leave this form? You'll lose what you've entered.  y leave  n keep editing`.

### Message actions menu (`.`)

```
                                        ╭─────────────────────────────────╮
                                        │  ☺ react                      r │
                                        │  ↩ reply in thread            t │
                                        │  ◆ save for later             s │
                                        │  ◷ remind me              ▸     │
                                        │  ● mark unread                u │
                                        │  ⧉ copy link                  l │
                                        │  ⧉ copy text                  c │
                                        │  ✎ edit                       e │
                                        │  ✗ delete                   del │
                                        ╰─────────────────────────────────╯
```

The menu is anchored at the selected message, on the right. Its keys also work without opening it. Delete is red and asks for confirmation through rush's confirm.

## DMs

```
  DIRECT MESSAGES  ☐ unread only  ✎ new      │
  ● Rogerio                            19:00 │      (selected DM's conversation,
    ╰ Just shipping a lot from the phone     │       same as a channel)
  ● sergio                             12:07 │
    ╰ you: oke sorry haha                    │
 ▍○ drew 🧙                            09:50 │
    ╰ you: back tomorrow                     │
  ● Manouk                            monday │
    ╰ but now …                              │
  ⁂ Manouk, Rogerio                   23 sep │
    ╰ langwatch.ai/event                     │
```

DM rows use rush's two-row agent layout: name, then a `╰` summary line in dim. The time is right-aligned and short: today shows the clock time, then `monday`, then `23 sep`. Unread rows are bright and bold.

## Activity

```
  ACTIVITY   [all] dms ·1  mentions ·3  threads      ☐ unread only        │
  today ────────────────────────────────────────────────────────────────  │
  ◇ Terrafied           # prod-infra-reviews  Production Terraform…  21:12 │  (selected item in
  ◇ Grafana Alerts      # prod-alerts-digest  WARNING · New Error…   21:04 │   context: the message
  ◇ GitHub              @Rogerio requested your review on fix(ev… 19:05 │   with a few around it,
  ● Rogerio             Just shipping a lot from the phone          19:00 │   or the thread)
 ▍@ Drew's assistant    # dev  @Alex could you review #8281 when… 15:06 │
  @ Drew's assistant    # dev  @Alex one small approval please: …  12:10 │
  ↩ sergio              ah ok, itdoesnt really matter tbh, i wi…   12:07 │
  yesterday ────────────────────────────────────────────────────────────  │
  @ Drew's assistant    # dev  @Alex the fix for the replicated…  yest  │
```

- The type glyph comes first, then the name.
- The channel is a dim `# dev` with no chip fill, since chips are reserved for things you can press.
- After that comes the text, with mentions coloured, and the time.
- Rows are grouped by day under section rules. The right pane is the item in context.

## Threads

```
  THREADS   [all] vip                                                        
  # dev  Rogerio, Drew's Orchardist and 3 others ─────────────────────────────
  ██ Alex  tue 14:14
     about the noise in attend channel
     ▏ Alex · posted in # prod-alerts-attend · 6 oct
     ▏ ^ can someone send an agent to fix the loki export failed errors? …
     ⋯ 100 more replies
  ██ PR Emoji Tagger  app  12:10
     langwatch/langwatch-pr-review-bot#19
     🚀 1  ☺+
  ██ Drew's assistant agent  app  15:06
     @Alex could you review #8281 when you have time? It splits the chart e2e…
     ╭ reply ──────────────────────────────────────────── ☐ also # dev ╮
     ╰ ❯ reply…                                                        ╯

  # dev  drew, sergio and you ───────────────────────────────────────────────
```

The composer collapses to a single row until it's focused. Forwarded messages and quotes sit behind a faint `▏` rail.

## Later

```
  LATER   [in progress] ·5  archived  completed
 ▍◆ # dev  Drew's assistant agent                         due today 17:00
    ╰ @Alex could you review #8281 when you have time?
  ◆ ● Manouk                                                    overdue 2d
    ╰ Can you send me a copy of your current 30% ruling beschikking…
```

Due times are dim, `due today` is yellow, and `overdue` is yellow and bold. `d` marks an item done, `a` archives it, and `r` sets a reminder.

## ctrl+k: jump and search

```
        ╭─ ⌕ Jump or search ─────────────────────────────────────── ctrl+k ─╮
        │ ❯ ▏where to, or what are you looking for                           │
        │  ⌕ in this channel   ◦ from me   ◦ includes me   ◦ has link   ◦ thre │
        │ ────────────────────────────────────────────────────────────────── │
        │ Unread & drafts ─────────────────────────────────────────────────  │
        │▍◆ Later                                                  5   enter │
        │  ✎ new-signups                                  draft              │
        │  # reached-plan-limit                                    3         │
        │  # support                                               1         │
        │  ⊡ agent-pr-reviewer                                     @1        │
        │  ⋯ 10 more                                                         │
        │                                                                    │
        │ Recent ──────────────────────────────────────────────────────────  │
        │  # prod-alerts-attend                                              │
        │  # general                                                         │
        │                                                                    │
        │ ↑↓ choose · enter go · tab filters · ctrl+f messages · esc close   │
        ╰────────────────────────────────────────────────────────────────────╯
          ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒
```

- The bar is rush's command bar, with the same parts:
  - the drop-down position
  - a frame shaded from orange to ember
  - the title set into the edge
  - the offset shadow
  - orange matched characters
  - aligned dim metadata
- Typing fuzzy-matches channels, people and DMs right away. After a 200 ms pause, it also searches messages, adding a `Messages` section.
- Slack's filter chips are rush's `barChip`s. `tab` focuses the chip row, and `space` toggles a chip.

## Claude (needs rush)

```
                         ✻ Hi Alex. Ask about your Slack.

            ╭ to Claude, through rush ──────────────────────────────── auto ╮
            │ ❯ what needs my attention today?                              │
            ╰ runs on your own agents · ctrl+o open in rush ────────────────╯

              ◇ what happened in Slack this week?
              ◇ summarise #prod-alerts-page since yesterday
              ◇ draft replies to my unanswered DMs
```

Once a conversation starts, it uses rush's conversation look (`▏` spine, folded steps, `✓`/`✗`), streamed from the plugin's `sessions.subscribe`. Without rush, the tab says `Claude needs rush · ? how to set it up` in dim.

## Rich text

Messages are Slack's mrkdwn, but bots and anything pasted from an editor arrive as markdown, so both are read (`internal/mrkdwn`). Where they disagree Slack wins: `*this*` is bold, because that's how everyone else in the channel sees it.

- **inline**: `*bold*` `**bold**` `_italic_` `~strike~` `~~strike~~`, `code` on a chip, `<links|label>`, `[label](https://…)` and bare URLs (blue, underlined, clickable through OSC 8), mentions (yours yellow on the ask ground, others blue), `:emoji:`.
- **headings**: `#` and `##` bold in bright, deeper ones bold in sub, with a blank row above.
- **lists**: `-` `*` `+` `•` bullets become `•` `◦` `▪` by depth, `1.` keeps its number, and wrapped lines hang under the text.
- **tables**: rush's layout. Two columns and five rows or fewer read as `key · value` lines; too narrow for columns, each row is its cells joined by ` · `; otherwise columns as wide as their widest cell, the widest giving up a cell at a time until it fits. The head row is bold with a faint rule under it.
- **code**: a ``` block sits on the panel ground, cut rather than wrapped at words. If the fence names a language (```go), it's highlighted with rush's highlighter (`photon/hl`: go, js/ts, python, rust, sh, json, yaml, toml, css, sql, ruby, the c family, lua, php, markdown) and the name sits dim in the top right.
- **quotes** `>` get a faint `▏` and sub text; **rules** `---` are a faint line.

Slack's own `rich_text` blocks, which most people's messages carry, are read into the same lines and drawn the same way, so a message looks alike whichever it came as. Emoji in them come with their characters, so they show even past the short table.

## Keys

You should never need the mouse. Keys follow rush where rush has one (ctrl+k, ctrl+n, `o`, the `▍` cursor, esc backing out a level at a time), and Slack's desktop keys for moving between conversations. The hint line shows the ones for wherever you are, and drops the least useful when it's narrow.

**Anywhere**

| key | does |
|---|---|
| `ctrl+k` (`/` in the sidebar) | jump to any conversation: what needs you, where you've just been and what's unread first, then fuzzy matching on everything. In the composer with text after the cursor, it cuts to the end of the line instead, as in rush |
| `ctrl+n` | the next conversation that needs you: a mention, or anything new in a DM. The hint line shows `ctrl+n N need you` in yellow while there are any |
| `alt+↑` `alt+↓` | the conversation above or below in the sidebar |
| `alt+shift+↑` `alt+shift+↓` | the unread conversation above or below |
| `alt+←` `alt+→` | back and forward through the conversations you've visited |
| `tab` `shift+tab` | sidebar, messages, composer |
| `f12` | the debug strip |
| `ctrl+alt+p` | profile for 35 s into the logs folder |

**Sidebar**: `↑↓` or `j k`, `pgup pgdn` by ten, `g G` first and last, `n` next unread, `enter` opens it and puts you in the composer, `q` quits.

**Messages**: there's a cursor, and it starts on the newest message.

| key | does |
|---|---|
| `↑↓` `j k`, `pgup pgdn` | a message, or about a page of them. Going past the oldest fetches older |
| `{` `}` | to the start of this run of messages by one person, then the run before; the next run |
| `n` | the first message you hadn't read when you opened it |
| `@` | the previous mention of you (or @here, @channel), wrapping round |
| `g` `G` | oldest, newest |
| `e` | edit it, if it's yours |
| `d d` or `delete delete` | bin it, if it's yours |
| `c` `l` | copy its text, copy a link to it |
| `o` | open its first link (http, https and mailto only) |
| `esc` | drop the cursor and go to the newest; again for the sidebar |
| `i` `a` `enter` | write |

**Composer**: `enter` sends, `shift+enter` (or `alt+enter`, `ctrl+j`) is a new line, `↑` in an empty box edits your last message, `esc` cancels an edit or goes to the messages, `ctrl+w` drops a word. A draft stays with its conversation when you go elsewhere.

Editing: `ctrl+a` `ctrl+e` (or `home` `end`) go to the start and end of the line, `ctrl+←` `ctrl+→` (or `alt+b` `alt+f`, since `alt+←→` are back and forward) move by word, `↑` `↓` move between the lines of a longer message, `ctrl+u` cuts to the start of the line and `ctrl+k` to the end. The box grows to six lines, then scrolls to keep the cursor in view. `shift+enter` needs a terminal that reports it (kitty, wezterm, ghostty, iTerm2 with CSI u); `alt+enter` and `ctrl+j` work everywhere.

**Mentions**: `@` or `#` after a space (or at the start) opens a list above the box, narrowing as you type. `@` offers people by handle and display name, those in the open conversation and your recent DMs first, bots last and only once you've typed something, deactivated people not at all, plus `@here`, `@channel` and `@everyone` outside DMs. `#` offers the channels you're in. `↑↓` choose, `tab` or `enter` accept, `esc` dismisses it until you start another. The box keeps the readable `@Alex` or `#general`; sending encodes it as `<@U123>`, `<#C123>` or `<!here>`, and escapes `&` `<` `>` in the rest. A mention is one piece: backspace takes all of it, and typing inside one turns it into plain text. Editing a message decodes every `<…>` into a piece, so links and user groups go back exactly as they came. Names are matched a word at a time, so a space ends the query.

Not yet: thread keys (`t`, `→`), reactions (`r`), save for later (`s`), mark unread (`u`), the `.` menu, user rebinding (rush's keymap file) and rush's `ctrl+]` leader for terminals that eat alt.

## Notices

Status flashes go in the hint line for 6 s, as in rush. Examples: `sent`, `copied link`, `✗ couldn't send · r retry`.

The connection state lives in header row 1:
- `● live` in green
- `◌ reconnecting 3s` in yellow
- signed out, loafer closes and asks you to sign in again, then reopens

## Sizes

- **Under 100 columns**: the sidebar collapses to icons and counts (`# 2`, `⊡`), and Enter opens a full-width list.
- **Under 70 columns**: one pane at a time, with esc going back.
- **Header**: drops the counts first, then the wordmark.
