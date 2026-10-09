# loafer UI

Slack's layout and features, drawn in rush's visual language. If a Slack element and a rush idiom disagree, keep Slack's *structure* (what's where, what it means) and rush's *rendering* (glyphs, greys, fills, spacing, hints). The rush catalogue this is based on: `rush/internal/ui/style.go`, `convo/style.go`, `ui/cmdbar.go`, `ui/dialog.go`, `ui/inputbox.go`; the Slack reference is the desktop app (screenshots of 2026-10-08).

## Rules

1. **One ground, four greys, one accent.** rush's palette and theme mapping as is (`theme.Ground`, `Ink/Surface/Accent`, colour-blind mode). Text 226,221,211; sub 168,162,152; dim 122,117,108; faint 72,68,63. Accent is rush's terracotta 217,119,87.
2. **Colour is state, never decoration.**
   - orange: focus, you, selection rail, matched characters
   - yellow: needs you, meaning mentions, @you, due today
   - blue: links, #channels and @others
   - green: ok, primary buttons, "recovered"
   - red: failure, danger buttons, delete, overdue
   - lavender: drafts and scheduled
   - Slack's attachment colours are an exception: they're content, drawn as a narrow `▌` bar mapped through `theme.Accent` so they stay readable on any ground.
2a. **Workspace colour.** Slack's aubergine is an accent, never a ground: the header, the tab row, the sidebar and the panes all sit on the terminal's own neutral ground (`theme.Ground`), and the aubergine, lifted to a plum (`Palette.Brand`) through `theme.Accent` so it reads on dark and light grounds alike, marks only the wordmark, the active tab and the open conversation in the sidebar. A workspace's own colour (your sidebar theme's, else one of a few dark colours picked by a hash of the team id) is its chip on the workspace rail and nowhere else.
3. **Bold is for what wants you**: unread channel names, mention counts, author names, section titles, key names in hints. Read things are plain; muted things are faint.
4. **No borders around content.** Hierarchy comes from greys, one-row gaps and fills. Rounded boxes only for things you type into and things floating over the screen: the composer, ctrl+k, modals and menus.
5. **Every list row has a marker column** and the same selection: a `selBG` fill plus an orange `▍` in column 0. Hover is `hoverBG`.
6. **Hint line grammar**: `key label  ·  key label`. The key is bold sub, the label dim, and the separator a faint `·` with two spaces each side. At most 5 pairs, the few that matter where you are (the `.` menu and the Keys below hold the rest), and the least important drop first when narrow.
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
| new-messages line | `──── new ──` | orange label, rule half toward the ground |
| day divider | `──── today ────` | sub label, faint rule |
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
- **Colours.** All pass through `theme.Accent`, so they hold their contrast on light grounds. The aubergine is lifted to a muted plum (about 653866) on dark grounds.
- **Narrow terminals.** Under 100 columns the logo is dropped and only the wordmark shows.
- **Notifier icon.** The notifier app's icon is the bands shoe, rendered as a PNG.

## Welcome

The first frame normally comes from the cache. When there's none (the first run, or a workspace just signed in to with `loafer login`) there's nothing to draw yet, and a big workspace's boot takes a while, so loafer shows this instead, on the neutral ground with the wordmark in plum:

```
                                ▄▄▄
                          ▄██▄▄▀▀▀▀▀▀▄▄▄
                          ████████████████▄▄
                          ████▀▀▀▀▀▀▀▀▀▀▀▀▀▀

                        welcome to loafer, sam
      Crumb & Co is new to loafer, so it's fetching it all once.
          next time it opens from the cache, straight away.

               ✓ signed in
               ✓ the workspace         9 conversations
               ◌ people                1,500 so far
               ✓ channels and sections
               ✓ unread counts
               ◌ emoji

             ctrl+k   jump to anything
             ctrl+n   the next thing that needs you
             tab      sidebar, messages, the box
             alt+1…5  home, dms, activity, later, claude
             .        all you can do with a message

               any key to start · the rest keeps coming
```

- **The logo** is the bands shoe, drawn in with `lace` (18 frames at 60 ms) and then still. That's the only timer it has.
- **The steps** are boot's own (`store.Progress`), ticked as each call answers: `client.userBoot`, `users.list` (counting people page by page), `users.channelSections.list`, `client.counts` (which waits for boot, so it's `·` until then) and `emoji.list`. `◌` is under way, `✓` done, `✗` failed. One that fails and isn't needed says `couldn't, carrying on`; boot and counts say `not yet, trying again`, and the footer says slack couldn't be reached, while the socket keeps trying and boots again once it gets through. Your name appears once boot knows it.
- **When it goes**: once every step has answered, the app comes up with the first conversation open. Once the sidebar can draw (boot, counts and sections in), `any key` goes straight there and the rest keeps arriving behind it; before that, keys do nothing but `ctrl+c`. Pictures and custom emoji images never hold it up.
- **Signed out**: `✗ slack signed you out · any key to sign in again`, and a key goes back to the sign-in, as the app would.
- **Drawn** only when something it shows changes (a step, a count, the size, the ground, a frame of the logo), and kept otherwise.
- **Sizes**: it's centred. Short of room it puts the keys on one line (`ctrl+k jump · ctrl+n what needs you · tab move`), then drops the logo, then the intro, then the keys.
- **A warm cache never shows it.** To see it anyway, `LOAFER_WELCOME=1 loafer` shows it on any start and waits for a key once ready, and `LOAFER_WELCOME=1 loafer --demo` does the same on the made-up workspace, whose fake Slack is slowed to answer over about four seconds so the steps can be watched.

## Main screen (Home, channel open, thread closed)

```
 loafer  LangWatch    Home  DMs ·2  Activity ·3  Later ·5  Claude       @ 3 mentions   12 unread   ● live
─────────────────────────────────────────────────────────────────────────────────────────────────────────────
                             │ # prod-alerts-page   someone must look now
                             │  Gr   Grafana Alerts  app  20:49
  ▾ 🚨 big bong              │       ▌ What happened: 37 groups are blocked at once (page threshold
  # opensource-alerts        │       ▌ 5+). A single hand-unblockable group no longer pages…
  # prod-alerts-attend       │       ▌ ⋯ show more
▍ # prod-alerts-page         │
  # prod-infra-reviews     1 │ ──────────────────────────────────────────────────────────── new ──
                             │  Gr   Grafana Alerts  app  21:49            ☺ react · ↩ reply · ⋯
  ▸ Team  4                  │       ▌ 🚨 CRITICAL · Blocked Groups Growing
  ▸ Social  3                │       ▌ What happened: 37 groups are blocked at once…
                             │        👍🏻 2   👀 1
  ▾ good-boys                │
  ⊡ agent-kanban-keeper      │  Dr   drew  21:52
  ⊡ agent-support          2 │       here's the bit I'm least sure of:
                             │        func touch(conv string) {        go
  ▾ Engineering              │            s.clock++
  # dev                   @2 │        }
  # github-issues            │
  ↓ 4 more unread            │ ╭─────────────────────────────────────────────────────────────────────╮
                             │ │ ❯ a message for #prod-alerts-page                                   │
                             │ ╰─────────────────────────────────────────────────────────────────────╯
  ctrl+n 2 need you  ·  enter open  ·  n next unread  ·  tab messages  ·  q quit
```

Notes:
- **Ground.** Everything sits on the terminal's own neutral ground: the header, the sidebar and the panes alike, divided by faint rules. Slack's aubergine is an accent, never a ground: lifted to a plum (`Palette.Brand`, 184,128,186 on the dark ground, through `theme.Accent` on others) for the wordmark, the active tab's label and the open conversation's `▍` in the sidebar.
- **Header**, one row plus a faint rule. Left: the wordmark (bold italic `loafer`, plum), the workspace in sub, then Slack's left rail as rush's top tabs (the active one on the `selBG` chip, its label bold plum; badges `·N`, yellow when they're mentions). Right: `@ 3 mentions` yellow bold, `12 unread` dim, and the connection state. Narrow, the counts shrink to `@3`, then go, then the wordmark goes. Not yet: the logo beside it.
- **Workspace rail**, only when you're signed in to more than one: four columns down the far left, a step darker than the ground. Each workspace is its initials on its colour (`CC` for Crumb & Co, `La` for LangWatch), the one shown marked with the orange `▍`, and under them its mentions (`@2` in yellow, `@+` past nine), else `•` when anything's unread, or a red `✗` once it's signed out. That chip is the only place a workspace's own colour shows.
- **Sidebar** = rush's list pane, with a row of air at the top.
  - Section headings are plain labels, as Slack's: a faint caret (`▾`, `▸` folded) in the glyph column, the name dim, with the section's emoji when the user gave it one, and no rule. A folded section is `▸ Team  4` with how many it hides in faint, and still lists what has unread, what mentions you and the conversation that's open. A blank row comes before a heading only after a conversation, so folded sections stack tight. Not yet: quiet folded sections joined into one line.
  - Rows: the glyph in dim, unread names bright and bold, read ones plain sub, muted ones faint (and not counted in the header or by `ctrl+n`). Mentions are `@2` in yellow bold on the right. The open conversation is on the `selBG` fill with a plum `▍`; the cursor is the orange `▍` (and the hover fill while the sidebar has focus).
  - `↓ 4 more unread` and `↑ …` at the edges are Slack's "More unread messages" pills, drawn as rush's `↓ N more` chip.
- **Avatars**: every author's is the same 4×2 cells (about square), beside the name and the first line, so the column reads as one thing. Where the terminal draws pictures (kitty, Ghostty) it's the picture, fitted into the 4×2 and centred. Elsewhere, and until the picture lands, it's Slack's letter avatar: the initials bold in near-white on the top row of a 4×2 tile tinted from the user's id (one of six muted shades). One that couldn't be fetched is tried once more when it's next drawn, a minute or more on. The pictures on disk are kept under 64 MB, the oldest going when loafer starts.
- **Pane header**: rush's chrome-filled block. It shows the channel, its topic dim, members `⊙ 4`, saved `◆`, and search `⌕`. One tab row, with the active tab underlined in orange.
- **Messages**:
  - **Header.** The avatar, then the bold name, `app` dim, and the time dim; the body and everything under it hang at the same 5-cell gutter. Consecutive messages from the same author within 5 minutes drop the header, and their time shows only when selected.
  - **Dividers** are one family: a thin `─` rule across the pane, one cell in at each end, with its label set into it. **Day dividers** centre the day (`today`, `yesterday`, `monday, 6 october`) in bold sub on a faint rule. **The "new" line** sets `new` in bold orange near the right (`──── new ──`) on a rule of orange taken halfway to the ground (`Palette.NewRule`). The current day sticks to the top as a chrome chip.
  - **The newer pill**: scrolled up, or left back in time by a search, a chip sits centred on the list's last row, `↓ 3 new messages  G` (what's come since you last read it, not counting yours), or `↓ newer messages` where that can't be told. The key is `G` in the messages and `ctrl+end` elsewhere.
  - **Read** is marked as you watch it come in: at the newest, in a focused terminal. A loafer in the background reads nothing until it has focus again.
  - **Attachments and blocks** sit behind a `▌` in their colour. Fields lay out in two columns when there's room. The footer is dim with ` · `. Long bodies fold at 8 rows to `⋯ show more`, in blue.
  - **Code blocks** are a `panelBG` panel as wide as their widest line (32 to 100 cells, never past the message), one cell of padding each side, highlighted with rush's highlighter; the language is dim at the panel's top right. Inline code is cText on the chip fill.
  - **Reactions** are Slack's pills: ` 👍🏻 2 ` on the chip fill with the count in sub; yours on the warm `askBG` with the count orange and bold. A space between pills, and they wrap a whole pill at a time. Skin tones (`+1::skin-tone-2`) draw as the toned emoji, custom ones as their picture or `:name:` dim. A reaction arriving draws only its own message again.
  - **Hover**: the message under the pointer takes the `hoverBG` fill, and `☺ react · ↩ reply · ⋯` sits dim at the right of its first row; a click on one does what `r`, `t` or `.` do, and a click anywhere on a message picks it. The list notes which message each of its rows is as it draws, so a motion is a lookup, one within the same message keeps the last frame, and the hover is a restyled copy of the message's kept rows: no message is drawn again for it. Leaving the list or the terminal losing focus lets it go.
  - **Selected message**: `selBG` fill plus the orange `▍`, like any row. Its actions are in the hint line and the `.` menu.
- **Composer**: rush's input box, quietened. The rounded edge is faint (`Edge`), orange when focused, and carries no labels in the usual case: the placeholder says where it goes (`a message for # dev`) and the hint line what enter does. The top edge says `editing your message`, `reply in thread` (with the `☐ also #channel` tick on the right) or an attachment's question when there is one. Inside are the `❯` and a faint placeholder.
- Mockups further down draw an avatar as a short `██`; it is the same 4×2 as here.
- **Hint line**: at most five pairs, for where you are, the rest being in the `.` menu and below (Keys); `ctrl+n N need you` leads in yellow while there are any. In the sidebar `enter open · n next unread · tab messages · q quit`; on a message `. actions · t thread · r react · esc newest`; in the box `enter send · shift+enter new line · ctrl+o attach · esc messages`.

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

- The thread is rush's side pane: rush's `sideWidth()` rule (28% of the width, at least 30 columns, at most 64). Under 100 columns it replaces the channel, and esc comes back; any key that goes to the channel (`←`, `i` in the sidebar) closes it first, so focus is never on what's hidden. The mouse wheel scrolls whichever the pointer's over. Not yet: resizing by dragging the `│`.
- Thread summaries under a message: `↩ 4 replies · last 16:47` in blue, then the first few repliers' avatars as 1-cell pictures where the terminal draws them.
- After the socket drops, the thread looked at last is fetched again, as the conversation is.
- For now the parent's own `↩ 4 replies` line stands where the "N replies" section rule goes.
- Replies arrive live, as do edits, deletes and reactions in the thread, and it's marked read (`subscriptions.thread.mark`) as its newest reply shows.

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
- **Choosing a button**: in a selected message, `b` / `shift+b` step through its buttons and menus (`tab` stays the pane key), and the chosen one goes orange. `enter` presses it, `esc` lets it go, and `✻ label` sits in the hint line until Slack answers.
- **Selects and overflow menus** are a chip with `▾`; `enter` opens a small chooser (`↑↓`, `enter`, `esc`). A **datepicker** takes the date typed, as `2026-10-09`. Other selects (users, channels, external) and checkboxes in a message say they can't be worked yet.
- Whatever the app does next comes down the websocket: an edit to the message, or a modal (below).
- **Context** blocks are dim.
- **Dividers** are a faint rule inside the `▌`.
- **Images** (image blocks, attachments' images, image files) are kitty placeholders up to 8 rows tall, with their `▣ name 1200×800` line (or the file's line) under them as a caption; with graphics off it's just that line. One on its way holds the rows it'll take when its size is known, so nothing jumps when it lands. A context block's images are a picture a row high beside its text. Behind an overlay, pictures go blank.
- **Headers** are bold and bright, with a blank row above unless they come first. **Sections** are their text, then their fields, two columns when each gets 20 cells, then the accessory as its chip.
- A message with blocks shows the blocks and not its text, as Slack does: the text is the notification's fallback. If none of its blocks can be drawn, the text shows instead. One that can't (an `input`, a `table`) is a faint `unsupported block (input)`.
- **Legacy attachments**: the pretext above the bar, then inside it the author, the title (a link), the text, fields (short ones two to a row), an image, any blocks, and the footer with its time.
- **Files** are a line each: `▣` image, `▶` video, `♪` audio, `▤` the rest, then the name (a link to it in Slack), its type and size, dim. A deleted file is a faint `▤ this file was deleted`.

Not yet: buttons inside attachments and inside modals, and the `✻` spinning.

Reactions: chips with the emoji and count. Yours sit on a warm chip with an orange count, others dim. `r` on the message adds one, `1`-`9` toggles one that's there. Custom emoji are 2×1-cell images, or `:name:` in dim.

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
- It opens when Slack pushes `view_opened` for a press made here (by its `client_token`), stacks on `view_pushed`, changes on `view_updated` and goes on `view_closed`.
- Inputs: plain text and datepickers are a filled row you type into (at the end only, for now), a static select is a chip that opens the chooser, and checkboxes and radio buttons are a `☐ ☑` or `○ ◉` line each. Optional ones say so. Any other block draws as it would in a message.
- Keys: `tab` `shift+tab` move between the inputs, then the submit and close buttons, the focused one marked with the orange `▍`. `↑↓` move within checkboxes and radio buttons, `space` or `enter` ticks one, `enter` on a button presses it, and `ctrl+enter` (or `ctrl+s`, for terminals that don't send it) submits.
- Submitting checks the required inputs and the date first, then sends `views.submit`; what the app says is wrong shows in red under each input, and the modal closes once it's taken.
- Leaving with edits turns the edge yellow and asks: `leave this form? you'll lose what you've entered`, `y` leave, `n` keep editing.
- Not yet: the `ctrl+o ↗` link, callouts' rails, and rush input boxes proper.

### Message actions menu (`.`)

```
                                        ╭─ Actions ────────────────────────╮
                                        │ ▍ ☺ react                     r  │
                                        │   ↩ reply in thread           t  │
                                        │   ◇ ask Claude                a  │
                                        │   ◆ save for later            s  │
                                        │   ◷ remind me ▸               m  │
                                        │   ⚑ pin                       P  │
                                        │   ● mark unread from here     u  │
                                        │   ⧉ copy link                 l  │
                                        │   ⧉ copy text                 c  │
                                        │   ↗ open link                 o  │
                                        │   ✎ edit                      e  │
                                        │   ✗ delete                    d  │
                                        ╰─ ↑↓ · enter · esc ───────────────╯
```

The menu floats over the right of the conversation, with the rest gone faint. `↑↓` and `enter` choose, an item's own key does it at once, `esc` closes. Every key works without opening it, so the menu is how you learn them. It lists only what applies: edit and delete are yours only, open link needs a link, and the thread pane's menu has no reply in thread or mark unread. Delete is red and works as `d d` does: the menu's `d` is the first press.

- **Copy text and open link** use what's drawn, blocks, rich text, attachments and files included (not the fallback text Slack keeps for notifications), the same renderer laid out wide so nothing wraps. The bars and the code's language tag are left out. Open link takes the first http, https or mailto link in it, a button's included.
- **Save for later** (`s`) puts the message on the Later list, and the menu then says `remove from later`. Whether it's there is only known once the list has been fetched, so the first `.` or `s` fetches it. A message with files also lists `download files` (`D`).
- **Remind me** (`m`) is a small chooser: `1` in 20 minutes, `2` in 1 hour, `3` in 3 hours, `4` tomorrow at 9, `5` next week (Monday, at 9), each with its time. It saves the message for later with a due time, as Slack's own "remind me about this" does now, so it shows in Later as due; a message already saved just gets the new time.
- **Pin** (`P`) pins it for the conversation, or unpins. A pinned message has a dim `⚑ pinned` line under it, and pins from other devices arrive live.
- **Mark unread from here** (`u`) moves the read marker to the message before it, so it and everything after are unread, in the sidebar too, and the `new` line moves to it. It stays that way (watching new messages arrive doesn't read it) until the conversation is opened again. Not in threads.

Saves, pins and marks change here at once and are put back if Slack refuses.

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

The list goes where the sidebar is, newest first, and `enter` opens the DM beside it with you in the composer. The 25 newest have their latest message fetched the first time the tab opens; the rest get one when something arrives. `n` starts a new DM (see Managing conversations). Not yet: unread only.

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
- Rows are grouped by day under section rules. Unread ones are bold, and the tab's badge counts them.
- `enter` marks it read and opens its conversation beside the list with the cursor on the message (a reply's thread parent, until threads come).
- Not yet: the dms, mentions and threads filters, and unread only.

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

Due times are dim, `due today` is yellow, and `overdue` is red and bold. `enter` goes to the message, `d` marks it done and `x` takes it off the list. Not yet: the archived and completed lists, and archiving. Reminders are set from a message (`m`).

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

## Managing conversations

```
        ╭─ # Browse channels ──────────────────────────────────────────────╮
        │ ❯ book▏                                                          │
        │ ──────────────────────────────────────────────────────────────── │
        │▍# bookclub  one chapter a fortnight                         7 ⊙  │
        │ ↑↓ choose · enter preview · esc close                            │
        ╰──────────────────────────────────────────────────────────────────╯

  ╭ reading # bookclub · 7 members ──────────────────────────────────────╮
  │  join   one chapter a fortnight                                      │
  ╰ j or enter joins · esc back ─────────────────────────────────────────╯
```

- **Browse** (`b` in the sidebar, or `#` as the first thing typed in `ctrl+k`) lists the public channels you're not in, archived ones left out, with their purpose and member count. It's fetched when it opens (a page of 200 at a time, and kept for five minutes) and narrowed by fuzzy match as you type. `enter` reads the channel with no box and no sidebar entry: where the box goes is a green `join` chip, which `tab` (or `i`) reaches and `j` or `enter` presses. Joined, it lands in Channels and the box appears. Going elsewhere lets the preview go, and it isn't marked read.
- **New message** (`N` in the sidebar, `n` in the DMs tab) is the same list over people, as `@` offers them. `enter` opens the DM with the highlighted person; `tab` picks several first (up to eight), and `enter` opens the group DM with them. A DM you already have just opens.
- **Sections**: with the cursor on a heading (the sidebar stops on them), `z`, `space` or `enter` folds or unfolds it; `z` on a conversation does it for its section and leaves the cursor on the heading. `s` on a conversation opens a chooser of your sections (and its kind's own, to put it back); `*` stars it, which moves it to Starred, and again to unstar it.
- **Mute**: `m` mutes or unmutes. Muted rows are faint, never bold, and don't count in the header or `ctrl+n`, nor notify.
- **Leave**: `x` on a channel asks in the hint line (`y` leaves, anything else keeps it); on a DM or group DM it closes it, and it comes back when someone writes.

Each applies at once and is sent to Slack in the background; if Slack says no, it's put back with the reason in the hint line (except a fold, which stays folded here). The websocket's `channel_joined`, `channel_left`, `im_close`, `pref_change` and `channel_section_*` events keep it in step with changes made elsewhere.

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

Once a conversation starts, it uses rush's conversation look (`▏` spine, folded steps, `✓`/`✗`), streamed from `rush session watch`. Without rush, the tab says `Claude needs rush · go install github.com/0xdeafcafe/rush/cmd/rush@latest` in dim.

## Rich text

Messages are Slack's mrkdwn, but bots and anything pasted from an editor arrive as markdown, so both are read (`internal/mrkdwn`). Where they disagree Slack wins: `*this*` is bold, because that's how everyone else in the channel sees it.

- **inline**: `*bold*` `**bold**` `_italic_` `~strike~` `~~strike~~`, `code` on a chip, `<links|label>`, `[label](https://…)` and bare URLs (blue, underlined, clickable through OSC 8), mentions (yours yellow on the ask ground, others blue), `:emoji:`.
- **headings**: `#` and `##` bold in bright, deeper ones bold in sub, with a blank row above.
- **lists**: `-` `*` `+` `•` bullets become `•` `◦` `▪` by depth, `1.` keeps its number, and wrapped lines hang under the text.
- **tables**: rush's layout. Two columns and five rows or fewer read as `key · value` lines; too narrow for columns, each row is its cells joined by ` · `; otherwise columns as wide as their widest cell, the widest giving up a cell at a time until it fits. The head row is bold with a faint rule under it.
- **code**: a ``` block sits on a panel as wide as its widest line (32 to 100 cells), cut rather than wrapped at words. If the fence names a language (```go), it's highlighted with rush's highlighter (`photon/hl`: go, js/ts, python, rust, sh, json, yaml, toml, css, sql, ruby, the c family, lua, php, markdown) and the name sits dim in the top right.
- **quotes** `>` get a faint `▏` and sub text; **rules** `---` are a faint line.

Slack's own `rich_text` blocks, which most people's messages carry, are read into the same lines and drawn the same way, so a message looks alike whichever it came as. Emoji in them come with their characters, so they show even past the short table.

## Keys

You should never need the mouse. Keys follow rush where rush has one (ctrl+k, ctrl+n, `o`, the `▍` cursor, esc backing out a level at a time), and Slack's desktop keys for moving between conversations. The hint line shows the ones for wherever you are, and drops the least useful when it's narrow.

**Anywhere**

| key | does |
|---|---|
| `ctrl+k` (`/` in the sidebar) | jump to any conversation: what needs you, where you've just been and what's unread first, then fuzzy matching on everything; `#` first browses the channels you're not in. In the composer with text after the cursor, it cuts to the end of the line instead, as in rush |
| `ctrl+n` | the next conversation that needs you: a mention, or anything new in a DM. The hint line shows `ctrl+n N need you` in yellow while there are any |
| `ctrl+f` (`/` in the messages) | search messages; see Search below. From ctrl+k, it searches for what's typed there |
| `alt+↑` `alt+↓` | the conversation above or below in the sidebar |
| `alt+shift+↑` `alt+shift+↓` | the unread conversation above or below |
| `alt+←` `alt+→` | back and forward through the conversations you've visited |
| `alt+1` … `alt+5` | the tabs: Home, DMs, Activity, Later, Claude. A tab's list is fetched the first time it opens |
| `alt+c` | the Claude tab, with the open conversation attached |
| `alt+w` | the next workspace, when you're signed in to more than one. Each keeps its place (what's open, drafts, scroll), so it's instant. ctrl+k lists the others too, under `Workspaces`, and typing matches their names |
| `tab` `shift+tab` | sidebar, messages, composer, and the thread's messages and box when it's open |
| `f12` | the debug strip |
| `ctrl+alt+p` | profile for 35 s into the logs folder |
| `ctrl+end` | the open conversation's newest message, reading it (outside the thread, where it's the thread's newest) |

**Sidebar**: `↑↓` or `j k`, `pgup pgdn` by ten, `g G` first and last, `n` next unread, `enter` opens it and puts you in the composer, `q` quits. Headings are stopped on. Managing it: `z` fold, `m` mute, `*` star, `s` move to a section, `x` leave or close, `b` browse channels, `N` new DM (see Managing conversations).

**DMs, Activity and Later lists**: `↑↓` or `j k`, `pgup pgdn`, `g G`, `enter` opens it, and in Later `d` done and `x` remove. `tab` goes to the conversation beside them, and `esc` there comes back.

**Messages**: there's a cursor, and it starts on the newest message.

| key | does |
|---|---|
| `↑↓` `j k`, `pgup pgdn` | a message, or about a page of them. Going past the oldest fetches older |
| `{` `}` | to the start of this run of messages by one person, then the run before; the next run |
| `n` | the first message you hadn't read when you opened it |
| `@` | the previous mention of you (or @here, @channel), wrapping round |
| `g` `G` | oldest, newest |
| `e` | edit it, if it's yours |
| `r` | react: a picker over every emoji, yours and the usual ones first. `enter` adds it, or takes it away if it's already yours (marked ✓) |
| `1`-`9` | toggle the message's nth reaction, in the order they're drawn |
| `d d` or `delete delete` | bin it, if it's yours |
| `.` | the actions menu: everything here, with its key |
| `c` `l` | copy its text as it's drawn, blocks and attachments included; copy a link to it |
| `o` | open its first link, as drawn (http, https and mailto only) |
| `D` `O` | save its files to `~/Downloads`, or save and open them with `open`. A name that's taken becomes `name (1).ext` |
| `s` | save for later, or take it off Later |
| `m` | remind me: in 20 minutes, 1 hour, 3 hours, tomorrow at 9 or next week |
| `P` | pin it, or unpin it |
| `u` | mark unread from here |
| `esc` | drop the cursor and go to the newest; again for the sidebar |
| `t` | open its thread on the right, or start one |
| `enter` `→` | open its thread, if it has one; else `enter` writes |
| `a` | ask Claude about it: the Claude tab, with it and five messages either side attached |
| `b` `shift+b` | step through its buttons and menus; `enter` presses the chosen one, `esc` lets it go (Block Kit, above) |
| `i` | write |

**Composer**: `enter` sends, `shift+enter` (or `alt+enter`, `ctrl+j`) is a new line, `↑` in an empty box edits your last message, `esc` cancels an edit or goes to the messages, `ctrl+w` drops a word. A draft stays with its conversation when you go elsewhere.

Editing: `ctrl+a` `ctrl+e` (or `home` `end`) go to the start and end of the line, `ctrl+←` `ctrl+→` (or `alt+b` `alt+f`, since `alt+←→` are back and forward) move by word, `↑` `↓` move between the lines of a longer message, `ctrl+u` cuts to the start of the line and `ctrl+k` to the end. The box grows to six lines, then scrolls to keep the cursor in view. `shift+enter` needs a terminal that reports it (kitty, wezterm, ghostty, iTerm2 with CSI u); `alt+enter` and `ctrl+j` work everywhere.

**Mentions**: `@` or `#` after a space (or at the start) opens a list above the box, narrowing as you type. `@` offers people by handle and display name, those in the open conversation and your recent DMs first, bots last and only once you've typed something, deactivated people not at all, plus `@here`, `@channel` and `@everyone` outside DMs, and the workspace's user groups by handle (`@bakers`, sent as `<!subteam^S123|@bakers>`; in messages a group reads as its handle). `#` offers the channels you're in. `↑↓` choose, `tab` or `enter` accept, `esc` dismisses it until you start another. The box keeps the readable `@Alex` or `#general`; sending encodes it as `<@U123>`, `<#C123>` or `<!here>`, and escapes `&` `<` `>` in the rest. A mention is one piece: backspace takes all of it, and typing inside one turns it into plain text. Editing a message decodes every `<…>` into a piece, so links and user groups go back exactly as they came. Names are matched a word at a time, so a space ends the query.

**Files**: `ctrl+o` in a box (the conversation's or a thread's) opens a prompt for a path, starting at `~/`. It lists what completes it as you type, dotfiles only once you type the dot. `↑↓` choose, `tab` completes (a folder, to look inside it), `enter` attaches what's typed, `esc` closes it. Pasting a path into the prompt works too. A path pasted into an empty box, which is what a terminal types for a file dropped on it (quoted, or with its spaces escaped, or several), asks `attach name?` in the box's top edge: `enter` attaches, any other key declines and the paste goes in as text. That's also what happens when it isn't a file. Attached files sit above the text as chips (`▤ name · size`), and `backspace` at the start of the box takes the last off. Each box has its own.

`enter` sends them, with what's written as their comment (a box of only chips sends too). The box empties, and a bar with a percent stands where the chips were while the file goes up, straight from disk and reported about ten times a second. When it's sent the message comes in over the websocket. One upload goes at a time. In a thread's box it's a reply, and `ctrl+b` sends it to the channel as well. A file past Slack's 1 GB limit, an empty one, or one that isn't a file is refused when you attach it, with the reason. If the upload fails, the chips and the words go back in the box.

On a message with files, `D` saves them to `~/Downloads` and `O` saves and opens them. Neither holds the file in memory.

**Emoji**: `:name:` in a message is drawn as the character, with aliases (`:thumbsup:`) and skin tones (`:+1::skin-tone-3:`) as Slack has them. Your workspace's custom emoji are 2-cell pictures where the terminal draws them, in text and reactions alike, and `:name:` in dim elsewhere or until they land. In the composer, `:` after a space and two letters opens the same list as mentions (`:sm` offers `:smile:`, `:smirk:`...), and `tab` or `enter` completes it. The picker puts what you've reacted with this session first, then Slack's usual dozen; that isn't kept between runs.

**Search**: a box over the screen, as ctrl+k's is. What's typed goes to Slack a quarter second after you stop, with Slack's modifiers as you type them (`in:#dev`, `from:@drew`, `before:2026-10-01`, `after:`, `is:thread`, `-word`), shown in blue. Opened from a conversation with nothing typed, it searches only there, as Slack's ⌘F does; `tab` switches between there and everywhere. Each result is its conversation, who and when, and a line or two with the matched words in orange (Slack's own marks, else the words you typed); replies say `↩ in a thread`. `↑↓` choose, `pgup pgdn` by five, and nearing the end fetches the next page. `enter` goes there with the cursor on it: at once if it's held, else after fetching the messages around it. A reply puts the cursor on its thread's parent and opens the thread with the cursor on the reply. Going past the newest from there (`↓`, `G`, `esc`) or sending fetches the newest again. `esc` closes it, and it opens again as it was left.

**People**: `p` on a message (or anywhere in a DM) opens a card over the screen for who wrote it: name, what they go by and pronouns, title, presence, status emoji and text with when it runs out, their local time from their zone, email where the workspace shows it, and their picture where the terminal draws them (initials on a tinted block elsewhere). `enter` (or `m`) opens your DM with them, asking Slack to make one if there isn't, `y` copies `@handle`, `esc` closes it. It reads the store as it draws, so a status or presence change lands in it, and `users.info` is asked for when it opens.

Presence is a dot on DMs in the sidebar, the DMs tab and a DM's header: `●` green while they're active, `○` faint when away, `●` dim until Slack has said. loafer sends `presence_sub` for the people in your DMs and the open conversation's authors, 100 ms after the last change, and applies `presence_change` (one user or a batch) and `manual_presence_change`. A flip redraws the dots and nothing else. A status emoji sits after the name in message headers, DM rows and a DM's header, and goes when it expires or is cleared; custom emoji statuses wait for images.

**Thread**: the pane is the conversation's list and box over again, so the cursor and its keys (`↑↓`, `e`, `r`, `d d`, `c` `l`, `o`) work there as they do in the conversation, over the parent and its replies, and `a` asks Claude about the whole thread. It opens with its box focused. `tab` and `shift+tab` come round to it after the conversation's box; `esc` in its messages closes it, back on the parent, and `←` goes to the conversation. In its box, `enter` replies, `ctrl+b` ticks "also send to the channel" (it unticks once sent), and `esc` cancels an edit or goes to its messages. What's written in a thread's box stays with that thread when it's closed. Opening another conversation closes it.

**Claude**: the box has the keys when the tab opens. With nothing asked yet, `↑↓` choose a starter (summarise, draft a reply, catch me up) and `enter` runs it; type and `enter` asks instead. `backspace` at the start of the box takes the last chip off. The answer streams in and has the keys once it's done: `i` puts it into its conversation's composer as a draft (after what's there, never sent), `↑↓` `pgup pgdn` scroll, and `enter` or typing goes back to the box. A follow-up goes to the same session; `alt+n` starts a new one, `ctrl+o` opens it in rush (`rush open <id> --hosted`, `ctrl+q` comes back), and `esc` goes from the box to the answer, then Home.

Not yet: a saved marker on messages, user rebinding (rush's keymap file) and rush's `ctrl+]` leader for terminals that eat alt.

## Notices

Status flashes go in the hint line for 6 s, as in rush. Examples: `sent`, `copied link`, `✗ couldn't send · r retry`.

The connection state lives in header row 1:
- `● live` in green
- `◌ reconnecting 3s` in yellow
- signed out, loafer closes and asks you to sign in again, then reopens. With several workspaces it only does that once every one is signed out: until then the one Slack let go says `✗ signed out · run loafer login` in its header, gets a red `✗` on the rail, and the flash says so wherever you are, while the rest carry on

**Typing.** `drew is typing…` (or `drew and sam are typing…`, then `several people are typing…`) sits in dim italics on the row above the composer for 5 s after the last `user_typing`, for the open conversation only. Typing in threads isn't shown. One tick is armed while someone is typing, none when nobody is.

**Notifications.** A new message notifies you when it's a DM or group DM, mentions you (or @here, @channel, unless you've silenced those there), has one of your highlight words, or replies in a thread you're in. It doesn't when it's yours, the conversation is muted or set to nothing, do not disturb or a snooze is on, or you have that conversation open in a focused terminal. Slack's own settings decide, so changing one in Slack applies at once.

It's shown through loafer's own notifier app when it's built, and clicking it goes to the message: loafer comes to the front in its terminal (the workspace's, on the Home tab, in the thread if it's a reply), or, closed, starts in the terminal it was last in and goes there. A burst's only brings loafer forward. The app is a few lines of Swift (`internal/notify/notifier.swift`) built with Xcode's command line tools into `~/.loafer/loafer.app`, with its Liquid Glass shoe icon (`internal/notify/icon`), and signed ad hoc, by `loafer notifyd install` and by loafer itself in the background when its source changes; the first notification asks you to allow them. Without it, it's the terminal's own escape where loafer knows it (OSC 9 for iTerm2 and WezTerm, OSC 777 for Ghostty, kitty's own for kitty), else `osascript`, and clicking those does nothing useful. Under tmux it's always the app or `osascript`. The title is `#channel` or the person (after the workspace's name, `Crumb & Co · #dev`, when there's more than one), the body is the message as plain text, and a burst is shown as one `N new messages` note every 3 s. loafer asks the terminal for focus events: in one that doesn't send them (tmux without `focus-events on`), the open conversation never notifies.

**While it's closed.** `loafer notifyd install` writes a LaunchAgent (`~/Library/LaunchAgents/com.github.0xdeafcafe.loafer.notifyd.plist`) and loads it; again, it reloads or restarts it, so a new build takes over. `uninstall` unloads and removes it. `status` says whether launchd has it, and who has the websocket: notifyd, the TUI (notifyd parked), or nobody. notifyd holds the default workspace's websocket with the same rules and the same 3 s bursts, shown through the app (or `osascript`), since it has no terminal. Nothing is ever in front of you, so focus doesn't count. Opening loafer takes the websocket over (notifyd parks it first) and closing it hands it back. Signed out, notifyd says `loafer was signed out; run loafer` once and waits for loafer to have been opened and closed before trying again. Its log is `~/Library/Logs/loafer/notifyd.jsonl`.

## Sizes

- **Under 100 columns**: the sidebar collapses to icons and counts (`# 2`, `⊡`), and Enter opens a full-width list.
- **Under 70 columns**: one pane at a time, with esc going back.
- **Header**: drops the counts first, then the wordmark.
