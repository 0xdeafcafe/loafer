# loafer design

A terminal Slack client in Go, in the same family as rush: same stack, same look, and the same habit of not burning your CPU and RAM. It replaces the desktop app for daily use.

Status: draft for review, 2026-10-08. API facts are in [slack-internal-api.md](slack-internal-api.md) and [slack-webapp-methods.md](slack-webapp-methods.md) (the second is still being written).

## Goals, in order

1. **Fast and light.** Idle means about 0% CPU. The first frame comes from the disk cache before the network answers. RAM stays small and bounded however long it runs.
2. **Feels like Slack.** Sidebar sections, unread and mention badges, the "New" line, threads, reactions, Block Kit buttons and modals, Activity, Threads, Later, jump and search.
3. **Easy to debug while dogfooding.** Every API call, websocket event and slow frame can be seen and replayed.

Not doing: huddles, calls, canvases, workflow builder, admin screens, file upload beyond a single file/paste (later), Enterprise Grid org switching (later).

## Budgets

These are checked by `loafer --soak` and benchmarks; a regression fails CI.

| What | Budget |
|---|---|
| Idle CPU (nothing arriving) | < 0.2% averaged over 60 s |
| Busy CPU (a channel at 10 msg/s) | < 3% |
| RSS, 300 channels, 5k users, 20 channels warm | < 45 MB |
| Cold start to first frame (from cache) | < 150 ms |
| Start to live (websocket hello) | < 1.5 s |
| Warm frame, 200x60, channel with 2k messages | < 1.5 ms, < 50 allocs |
| Switching channels (cached) | < 5 ms to frame |

## Shape

```
cmd/loafer            main; subcommands: (run), login, notifyd, app, plugin, report
internal/notify       Slack's notification rules + delivery; shared by the TUI and notifyd
internal/slackapp     the real Slack app: bot token, Socket Mode, shortcuts, bot DMs
notifier/             small Swift app for native notifications (built like rush's menu bar app)
internal/slack        transport, auth, API methods, websocket, event types
internal/slack/creds  read xoxc + d from the desktop app (leveldb, cookies db, keychain)
internal/store        the in-memory model + disk cache; the only owner of Slack state
internal/mrkdwn       mrkdwn and rich_text blocks -> styled spans
internal/blockkit     blocks/attachments/modals -> rows with hit targets
internal/ui           bubbletea model, screens, keys, command bar
internal/images       avatars and images: fetch, decode, cache (uses termimg)
internal/obs          event log, trace, metrics, pprof, report bundle
internal/rushlink     optional rush integration (AI features); off unless rush is found
github.com/0xdeafcafe/photon  shared with rush: canvas, cellw, frame, fuzzy, hl, jsonx, keychain, rows, termimg, theme, uithread
```

Dependencies stay as rush's (bubbletea v2, ultraviolet, x/ansi, x/image) plus three:
- `github.com/coder/websocket`, a small websocket library with context-aware API;
- `github.com/syndtr/goleveldb`, pure Go, for the desktop app's Local Storage (login only);
- `modernc.org/sqlite`, pure Go, for the cookies db (login only). `login` is a separate code path, so neither is touched by the running client. If its binary-size cost (~6 MB) bothers us, `login` can shell out to `sqlite3` instead, which macOS ships.

### Threads (goroutines)

- **UI goroutine**: bubbletea. Never does I/O, decoding or parsing. Guarded by rush's `uithread` stall watchdog (75 ms).
- **Socket goroutine** per workspace: reads frames, decodes into typed events, applies them to the store, sends one coalesced `changedMsg` to the UI (at most one per frame).
- **API workers**: a bounded pool (4) with per-method rate limiting and request coalescing (two screens asking for the same `users/info` share one call).
- **Image workers**: 2 slots, as in rush.

### Store

One owner of state, behind a mutex with short critical sections; the UI reads immutable snapshots.

- Users, channels, emoji: interned IDs, compact structs. Loaded from disk cache at start, refreshed from `client.userBoot` / `client.counts` / edge `users/info` incremental (`updated_ids`).
- Messages: per channel, a window of the most recent ~300 kept in RAM; older pages fetched on scroll and dropped when scrolled away. Only the 20 most recently viewed channels keep a window (LRU); the rest keep counts and the latest message only.
- Each message carries a version; rendered rows are cached against `(msg version, width, theme)`.
- Disk cache under `~/Library/Caches/loafer/<team>/`: users, channels, sections, emoji, and the last 50 messages of the 20 hottest channels. Written behind (debounced, off the UI goroutine), read once at start. JSON via jsonx. Credentials are never in the cache.

### Rendering

rush builds one big ANSI string and modals do Strip/Cut on every line behind them. loafer keeps rows as **styled spans** until the last moment instead:

- A `Row` is `[]Seg{text, style, width}`; width is computed once when the row is made.
- Each message renders to rows once per width and is cached; the message list is virtualised by a prefix sum of row heights (rush's Fenwick `rowIndex`), so only visible messages are touched.
- Panes (sidebar, main, thread) produce exactly `h` rows of exact width; the frame is the panes side by side.
- Overlays (ctrl+k, modals, menus) splice spans by column - no ANSI parsing - and dim the rows under them by swapping style, not text.
- The last step emits ANSI from spans, reusing one buffer, and hands bubbletea the string; bubbletea's cell diff does the rest. Frames that change nothing are skipped (rush's `sameFrame`).
- No timers while idle. Relative times ("Today at 21:49") change on the minute, so one timer is armed for the next minute boundary only when a visible row shows a time that will change.

### Real-time

`wss://wss-primary.slack.com/?token=…&gateway_server=<team>-1&slack_client=desktop&batch_presence_aware=1`, the `d` cookie on the handshake. Ping every 30 s when nothing has arrived; `reconnect_url` kept for fast reconnect; on `hello` with `fast_reconnect`, skip the reboot. After a gap, catch up with `client.counts` plus `conversations.history?oldest=` for warm channels only.

Typing indicators and presence are shown but throttled to one UI update per 250 ms.

### Images

Avatars and image attachments via termimg (kitty placeholders; blocks fallback; initials if graphics are off). Fetched at the cell-pixel size, decoded and scaled off-thread, stored on disk as small PNGs keyed by URL hash, at most ~300 decoded in RAM (LRU). Placeholders live in cached rows, so scrolling costs nothing.

## Screens

Left rail (narrow): Home, DMs, Activity, Later, Claude. One letter each plus a badge; collapses at small widths.

- **Home**: sidebar with Threads, Drafts & sent, then the user's sections (collapsible, unread bold, mentions badged, "More unread" hints at the edges). Main pane is the channel; `t` or Enter on a message opens its thread on the right.
- **Channel**: header (name, topic, member count), day dividers, the red "New" line at `last_read`, grouped messages (consecutive from the same author within 5 min collapse their header), reactions, "N replies" lines, Block Kit and attachments with their colour bar, "Show more" folding for long messages. Composer at the bottom with @-mention, #-channel and :emoji: completion.
- **Thread pane**: parent, replies, its own composer, "also send to #channel".
- **DMs**: list with avatar, name, preview, time; unread toggle.
- **Activity**: All / DMs / Mentions / Threads tabs; list on the left, the message in context on the right.
- **Threads**: followed threads with unread replies, inline reply.
- **Later**: saved items, in progress / archived / completed, with due reminders.
- **ctrl+k**: unread places, recent places, then fuzzy channels/DMs/people; typing words searches messages after a pause (`search.modules`), with filter chips (in this channel, from me, includes me, has link, threads only).
- **Claude**: the Slackbot replacement; see below.

Message actions (`.` or right-click): react, reply in thread, edit, delete, copy link, copy text, save for later, mark unread, remind me.

### Block Kit

Rendered: section, context, divider, header, image, actions, rich_text, fields, plus legacy attachments (colour bar, title, fields, footer). Interactive: buttons, overflow menus, static selects, datepicker as a text field. Clicking calls the web client's block-action endpoint; modals that open (from the response or a websocket view event) render in an overlay with inputs (plain text, select, checkboxes, radio) and submit/close. Leaving a modal with edits asks "Keep editing / Leave", as Slack does.

## Claude pane

Superseded in part by [claude-pane.md](claude-pane.md): loafer hands rush the text itself through `rush session`, with no plugin, once rush can stream an answer back.

rush is optional. loafer runs fully without it; the AI features (the Claude pane, "summarise this thread", "what needs my attention") need it, because they run on your own agents and accounts through rush rather than Slack's AI. At start loafer checks for `rush` on the PATH and that the loafer plugin is installed (one cheap `exec.LookPath` plus a stat, cached, never on the UI goroutine). Without them the AI entries still show in the rail and the ctrl+k bar, greyed, with one line on how to turn them on (`go install …/rush` then `loafer plugin install`). Nothing AI-related is imported into the client's hot path: the integration lives in `internal/rushlink` and only runs when used.

The pane starts and shows an agent session through rush. loafer ships one rush plugin (`plugin/plugin.json`, protocol "rush") that speaks rush's framed JSON-RPC on fd 3 from loafer's own code:

- **Tools** via `tools.list` / `tools.call`: read a channel or thread, search, list unreads and mentions, draft a reply (posting needs your confirmation in loafer), react. rush asks before each tool call.
- **Sessions** via `sessions.start` / `subscribe` / `send`: the Claude pane starts a session with the prompt and shows it in loafer; it also appears in rush's list.

Constraints from rush's plugin sandbox, and how we live with them:
- No Keychain, env wiped: rush runs `security` for the plugin, as an approved `exec`, to read loafer's own item ([rush-plugin.md](rush-plugin.md)).
- Network only through rush's HTTPS CONNECT proxy, exact hosts: `<team>.slack.com:443`, `edgeapi.slack.com:443`. The plugin uses HTTP only (no websocket), honouring `HTTPS_PROXY`.
- Binary in the plugin folder is covered by approval, so every rebuild needs `rush plugin approve loafer`; `loafer plugin install` copies the binary and runs check + approve.
- Session limits: 4 live, 30 starts an hour, cwd inside the declared workspace (a `~/.loafer/claude` scratch dir).
- macOS only, like rush's plugins.

Starter prompts mirror Slackbot's: "What needs my attention today?", "What happened in Slack this week?".

## Notifications

Slack desktop doesn't get Apple push; it gets everything over the same websocket. loafer does the same.

- **Rules** (`internal/notify`): from userBoot prefs and `pref_change` events. That means per-channel `all_notifications_prefs` (everything / mentions / nothing, plus thread replies) and mutes. It also covers `highlight_words` keywords, DND windows and snoozes from the dnd info, and whether you're the author.
- **Who delivers**: `loafer notifyd` runs as a launchd LaunchAgent (`loafer notifyd install`). It holds the websocket and a tiny store (users and channels only, no message windows), with a budget under 15 MB RSS and no timers beyond the socket ping. When the TUI starts, it takes over through a lock and a unix socket at `~/.loafer/run/notify.sock`; notifyd parks its socket and resumes when the TUI exits. There is only ever one connection. As built (`internal/notifyd`): every TUI holds `~/.loafer/run/tui.lock` shared for its life and says `take` on the socket before opening its websocket; notifyd answers `parked` once its own is closed, then blocks on the lock exclusively, so the kernel wakes it when the last TUI exits or crashes, with nothing polled. `notifyd.lock` keeps notifyd to one. Each time it goes live it reads the sign-in again into a fresh store. It boots as the TUI does, so it also fetches emoji and sections it doesn't need; the 15 MB budget isn't yet measured against a real workspace.
- **Delivery**: a small Swift app (`notifier/`, built with the Xcode CLT compiler on first run, as rush's menu bar app is) using UNUserNotificationCenter. You get loafer's own icon and its own settings entry in System Settings. Clicking opens loafer at the message: the running TUI is told over the socket, or the notifier opens your terminal with `loafer open <permalink>`. You can reply inline from the notification. `osascript` is the fallback if the app can't build. Not built yet: notifyd shows everything through `osascript`, and clicking does nothing.
- **Clearing**: on `channel_marked` / `thread_marked` from any device, delivered notifications for that channel are removed.
- **Presence**: loafer sends the same activity signals as the desktop app (`tickle`, presence sub), so Slack's mobile pushes keep their usual "not while you're active" behaviour.
- **Doubles**: if the Slack desktop app is running too, loafer stays quiet unless told otherwise (`notify.withDesktop`).

## Slack app (borrowed token + real app)

Two identities, each for what it's good at:

- **Borrowed session token (xoxc + d)**: everything done as you, including reading, posting, reacting, Activity, Later, sections and the websocket.
- **Real Slack app (bot xoxb + app-level xapp, Socket Mode)**: things that reach you from outside loafer, plus a connection that survives the borrowed token dying when you sign out of the desktop app.

What the app does:
- **Pushes to your phone**: the bot DMs you and Slack pushes it to your phone. It's used for rush agents waiting on you or finished (when rush is present), mentions while you're away from the terminal (optional, off by default), and Claude digests on a schedule.
- **Shortcuts from any Slack client, including mobile**: the message shortcuts "Ask Claude about this" and "Save to loafer", and a `/loafer` slash command. These arrive over Socket Mode at notifyd, which hands them to rush (AI) or the store.
- **App Home**: shows status (loafer connected, rush agents waiting) and a few buttons.
- **Health alert**: if the borrowed token stops working, the bot DMs you to run `loafer login` again.

Setup: `loafer app init` prints a ready-made app manifest (`slackapp/manifest.yaml`: Socket Mode on, bot scopes `chat:write`, `im:write`, `im:history`, `commands`, `users:read`; the shortcuts; the App Home messages tab). You create the app from it at api.slack.com, a workspace admin approves it, and you paste the bot and app tokens into `loafer app init`, which stores them in the Keychain and checks them with `auth.test`. Socket Mode is one more websocket, idle almost all the time, held by notifyd (or the TUI when notifyd isn't installed). The app never reads your messages; the borrowed token does that.

## Debugging and dogfooding

- **Event log**: `log/slog` JSON lines into a 4k-entry ring in memory and a rotating file at `~/Library/Logs/loafer/loafer.jsonl` (5 × 10 MB). Each API call logs method, ms, bytes, status, and rate-limit headers. Each websocket event logs type, channel, and lag (server ts to receive). Slow frames are logged, as are cache hits and misses and errors. Tokens and cookies are redacted by the transport before anything is logged.
- **Trace**: `LOAFER_TRACE=api,ws,render,store` (or `--trace`) also logs bodies and payloads (redacted). Off costs one atomic load.
- **Debug strip** (`F12`): heap/RSS, CPU %, goroutines, GC count, last/avg/max frame ms, skipped frames, socket state and lag, API in flight, cache sizes.
- **Event viewer** (`F11`): the live ring, filterable by kind, channel, method; Enter shows the payload.
- **Profiling**: `--pprof localhost:6061` serves net/http/pprof. `ctrl+alt+p` writes 30 s CPU, heap, goroutine, and a 5 s runtime/trace to `~/Library/Logs/loafer/prof/<time>/`. `MemProfileRate` stays 0 unless `LOAFER_MEMPROFILE=1`.
- **Stall watchdog**: rush's `uithread`; any UI goroutine block over 75 ms logs a stack.
- **Soak**: `loafer --soak 60s 200x60 --replay <events.jsonl>` runs the real UI headless against a recorded event stream and prints CPU, RSS, peak heap, and frame stats. Recordings come from `--record`, redacted.
- **Bug report**: `loafer report` zips the last logs, profiles, version, terminal and settings (secrets stripped) for a bug found while dogfooding.
- **Benchmarks**: message render, mrkdwn parse, list scroll, frame compose, event apply; numbers go in `docs/perf.md` with the commands to repeat them.

## Build order

1. **Skeleton and observability**: module, rush's primitives (now in photon), `obs` (log, trace, pprof, debug strip), soak harness.
2. **Login**: `loafer login` reads the desktop app's token and cookie, verifies with `auth.test`, stores them in the Keychain (`security add-generic-password`); a manual paste fallback.
3. **Slack app + notifyd skeleton**: `loafer app init`, Socket Mode connection, bot DM to yourself, `loafer notifyd install`, the notifier app; first use is the token-health alert and rush "agent waiting" pushes.
4. **Store and boot**: userBoot, counts, users, emoji, disk cache; first frame from cache.
5. **Home**: sidebar, channel view with mrkdwn and rich_text, composer, mark read, websocket live updates, reactions, edit/delete.
6. **Threads**: thread pane, then the Threads view.
7. **Block Kit**: render; then buttons; then modals.
8. **Images**: avatars, then attachments.
9. **DMs, Activity, Later**, from the captured endpoints.
10. **ctrl+k and search**.
11. **Notifications**: Slack's rules in `internal/notify`, TUI/notifyd hand-off, click to open, inline reply.
12. **AI features (optional, need rush)**: Claude pane and helpers via the rush plugin; greyed with setup hint when rush is absent.

Each step ends with benchmarks against the budgets and a dogfooding build.

## Open questions

- Method names for sections, Activity, Threads, Later, drafts, block actions and modals are confirmed from the desktop bundle ([slack-webapp-methods.md](slack-webapp-methods.md)). Some param keys are still unknown: `users.channelSections.list`, `activity.markRead`, `subscriptions.thread.mark`, `saved.list` paging, `views.submit`, `client.dms`, and the `search.modules` messages module. Fill them in by probing read-only calls with `--trace` and saving the redacted responses as test fixtures. Before shipping a write call, check it against a DevTools capture.
- Whether `login` can read the App Store build's data (sandbox container) without Full Disk Access; if not, a one-time paste from DevTools.
- Whether loafer's HTTP client works through rush's plugin proxy against Slack (untested).
- Who approves the Slack app in the LangWatch workspace.
