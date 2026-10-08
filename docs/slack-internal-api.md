# Slack internal/undocumented API - engineering reference

Compiled by reading source (not blog posts) from:

- **wee-slack** - [github.com/wee-slack/wee-slack](https://github.com/wee-slack/wee-slack), `slack/` package (Python)
- **slackdump** - [github.com/rusq/slackdump](https://github.com/rusq/slackdump) v4, `internal/edge/`, `auth/`
- **rusq/slack** - [github.com/rusq/slack](https://github.com/rusq/slack) (slack-go fork with internal endpoint types; vendored into slackdump)

Each item is tagged **CONFIRMED** (seen directly in one of the above) or **UNCERTAIN** (not found in the reviewed source; based on the task's own description, general knowledge, or indirect evidence - verify with a live DevTools capture before shipping).

---

## 1. Auth transport

**CONFIRMED.** Two independent client codebases agree on the shape:

- **Token**: sent as a normal POST form field `token=xoxc-...`, not as `Authorization: Bearer`, on slackdump's edge client. In `internal/edge/edge.go`:
  ```go
  func (cl *Client) PostFormRaw(ctx context.Context, url string, form url.Values) (*http.Response, error) {
      if form["token"] == nil {
          form.Set("token", cl.token)
      }
      ...
      req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  ```
  wee-slack instead sets it as a header: `slack/slack_api.py:_get_request_options()` builds `"httpheader": f"Authorization: Bearer {token}"`. Both work - Slack's web API accepts the token either as a form/query param or a Bearer header; the desktop client itself uses form-encoded `token=` for `client.*`/`chat.*` calls (seen in both the form-based edge client and the `BaseRequest{Token}` JSON body used for `edgeapi.slack.com`).
- **Cookie**: `Cookie: d=<value>` is the one cookie both projects require. slackdump's `auth.NewValueAuth` adds a companion `d-s=<unix-ts-10s>` cookie when the token is a client token (`auth/value.go`):
  ```go
  c.Cookie = []*http.Cookie{
      makeCookie("d", cookie),
      makeCookie("d-s", fmt.Sprintf("%d", time.Now().Unix()-10)),
  }
  ```
  wee-slack sends only `d` (and accepts a `d=...;d-s=...` combined cookie string via `get_cookies()` in `slack/util.py`, defaulting the bare value to `d` if no name is given).
- **User-Agent**: slackdump's edge `do()` sets `User-Agent: <slackauth.DefaultUserAgent>` and `Accept-Language: en-NZ,en-AU;q=0.9,en;q=0.8` on every request (`internal/edge/edge.go`). wee-slack sends `wee_slack <version>`. Neither is rejected by Slack for a non-browser UA in practice, but spoofing a real desktop UA reduces the chance of being fingerprinted.
- **Base URL**: workspace-scoped, not `slack.com/api/`. slackdump: `webclientAPI: fmt.Sprintf("https://%s.slack.com/api/", workspaceName)` (`internal/edge/edge.go:NewWithClient`), or derived from the `AuthTestResponse.URL` when already authenticated (`NewWithInfo`). wee-slack's `_fetch()` oddly posts to `https://api.slack.com/api/{method}` - Slack's edge accepts both the generic host and the team subdomain for xoxc+cookie auth as long as the cookie's domain matches, but the team subdomain is what the real desktop client uses and is the safer choice for multi-workspace correctness.
- **edge API** (`client/users/channels search etc.`) is a *separate* host: `https://edgeapi.slack.com/cache/<teamID or enterpriseID>/<method>`, JSON body (not form), `Content-Type: application/json`, same `token` field inside a `BaseRequest{Token}` JSON struct (`internal/edge/edge.go:PostJSON`, wee-slack `slack_api.py:SlackEdgeApi._fetch_edgeapi`). `teamID` for Enterprise Grid orgs is the **enterprise** ID, not the individual workspace's team ID - slackdump picks `enterpriseID or teamID` (`edge.go:NewWithInfo`).
- **Multi-workspace**: each workspace gets its own `Client`/`Workspace` instance holding its own token, cookie jar, and `webclientAPI` base URL built from that workspace's subdomain. wee-slack keeps one `SlackWorkspace` per configured workspace with independent config options (`api_token`, `api_cookies`) and token-type detection (`slack_workspace.py:token_type`, see §3 below). There is no shared session across workspaces; a single `d` cookie is often valid for every workspace under the same Slack org/browser login, but each workspace still needs its own xoxc token because tokens are workspace-scoped.

## 2. Getting credentials from the macOS desktop app

**Token (CONFIRMED mechanism, UNCERTAIN exact desktop-app path).** Slack's desktop app is Electron, and both the app and the real browser store the xoxc token the same way: `localStorage['localConfig_v2']`, a JSON blob keyed by team ID, each entry having a `.token` field. wee-slack's `extract_token_from_browser.py` reads this directly from a **browser's** Chromium LevelDB-backed Local Storage:
```python
leveldb_key = b"_https://app.slack.com\x00\x01localConfig_v2"
db = plyvel.DB(str(leveldb_path))      # plyvel = Python LevelDB bindings (cgo-equivalent, uses libleveldb)
local_storage_value = db.get(leveldb_key)
```
and the project's own manual-auth doc (slackdump `doc/login-manual.md`) gives the equivalent JS console one-liner:
```js
JSON.parse(localStorage.localConfig_v2).teams[<teamID>].token
```
Neither reviewed project reads the **Electron desktop app's** LevelDB store directly (`~/Library/Application Support/Slack/Local Storage/leveldb`) - only slackdump (via `rod`/CDP browser automation, see below) and wee-slack (via a real browser's profile) were found doing this. The key format should be the same (`_https://app.slack.com\x00\x01localConfig_v2` under the Electron app's own LevelDB, since the desktop app is just a Chromium origin for `app.slack.com`), but **this exact path has not been confirmed in source** - verify against the live LevelDB files (`~/Library/Application Support/Slack/Local Storage/leveldb/*.ldb` and `*.log`) before relying on it; LevelDB files are often locked while Slack is running, so a copy-then-open (as wee-slack does on `plyvel.IOError`, copying to a temp dir) is needed. **UNCERTAIN**: the Mac App Store sandboxed container path (`~/Library/Containers/com.tinyspeck.slackmacgap/Data/Library/Application Support/Slack/...`) was not found referenced in either codebase; it is a reasonable inference from macOS App Sandbox conventions but unverified here.

**Cookie decryption (CONFIRMED mechanism from wee-slack, for Chrome/Chromium on macOS; desktop-app Keychain entry name UNCERTAIN).** `extract_token_from_browser.py` implements the full Chromium cookie decrypt:
```python
chrome_key_iterations = 1003   # macOS; Linux uses 1
...
salt = b"saltysalt"
length = 16
key = PBKDF2(passwd, salt, length, chrome_key_iterations)   # PBKDF2-HMAC-SHA1 by default in pycryptodome
cipher = AESCipher(key)   # AES-128-CBC, fixed IV = b" "*16

def chrome_decrypt_cookie(cipher, cookies_version, encrypted_value):
    raw_value = cipher.decrypt(encrypted_value[3:])      # strip "v10"/"v11" prefix
    value = raw_value[32:] if cookies_version >= 24 else raw_value   # strip 32-byte SHA256 domain-binding prefix
    return value.decode("utf8")
```
This confirms: AES-128-CBC with a static space-byte IV, PBKDF2 (SHA1, pycryptodome default) with salt `"saltysalt"`, **1003 iterations on macOS** vs 1 on Linux, keyed by the OS keychain password for the "`<Browser> Safe Storage`" generic password item (code looks up `item.get_label() == "Chrome Safe Storage"` via `secretstorage`/D-Bus on Linux; macOS equivalent would be the Keychain item literally named `Chrome Safe Storage`, read via `security find-generic-password -w -s "Chrome Safe Storage"` or a Keychain Services binding - **this script's macOS path for fetching that password was not found in the file**: it only shows the Linux `secretstorage` lookup and a `peanuts` fallback, so the macOS Keychain-read step is **UNCERTAIN/not confirmed in this source** even though the surrounding crypto is confirmed). For the desktop app rather than a browser, the Keychain entry is expected by analogy to be named `Slack Safe Storage` - this specific name is **UNCERTAIN**, not seen in any reviewed source, and should be confirmed with `security dump-keychain` or `security find-generic-password -s "Slack Safe Storage"` on a real machine before relying on it.

The 32-byte-prefix handling (`cookies_version >= 24`) is exactly the mechanism the task refers to as "the newer 32-byte SHA256 host prefix in Chromium >= 130" - **confirmed in code**, though wee-slack gates it on the Cookies DB schema `version` row, not a Chromium build number; the two roughly track together.

**slackdump does not do any of this.** `rusq/slackdump`'s primary auth path (`auth/rod.go`, `RodAuth`) drives a real or headless browser via the `rod` library over the Chrome DevTools Protocol, logs in interactively or headlessly, and extracts the resulting token/cookies from the live browser session rather than parsing LevelDB/SQLite files at rest. Its documented manual fallback (`doc/login-manual.md`) also tells the user to copy the token/cookie out of DevTools by hand rather than automating file-level decryption. slackdump does implement a cookie→token exchange once you *have* the `d` cookie, hitting `https://<workspace>.slack.com/ssb/redirect` with `Cookie: d=<value>` to get back a fresh xoxc token (`auth/token.go:getTokenByCookie`) - useful if you only have the cookie and need the token.

**Pure-Go libraries** (general knowledge, not from the reviewed repos, since both reference projects are Python/cgo-adjacent): `github.com/syndtr/goleveldb` (pure Go LevelDB reader, no cgo) for Local Storage, and `modernc.org/sqlite` or `github.com/glebarez/sqlite` (pure-Go, cgo-free SQLite drivers) for the Cookies DB. **UNCERTAIN** - pick/verify against current maintenance status before depending on them.

## 3. Startup: client.userBoot / client.counts / channel sections

**CONFIRMED.** wee-slack calls both on connect (`slack_api.py` methods `fetch_client_userboot` → `client.userBoot`, `fetch_client_counts` → `client.counts`). slackdump's Go types (`internal/edge/client_boot.go`, `internal/edge/client.go`) are posted to **`client.init`** (confirmed alias/current endpoint name for `client.userBoot` - the struct's doc-comment literally says `// "client.userBoot"` while the code posts to path `"client.init"`, i.e. Slack renamed/aliased the wire path but kept the historical type name) and `client.counts` respectively.

`client.userBoot` response (`ClientUserBootResponse`) includes: `self` (own user), `team`, `ims` (all DM conversations), `workspaces`, `prefs` (map of user preferences), `subteams` (user groups), `starred`, `channels` (full channel/group list the user is in, each with `topic`, `purpose`, `members`, `is_member`, `last_read`, `latest`), `read_only_channels`, `dnd`, cache-busting timestamps (`emoji_cache_ts`, `translations_cache_ts`). Request form includes `min_channel_updated`, `version_ts`/`build_version_ts` (slackdump sends `time.Now().Add(24h)` to force a full payload) and the generic `WebClientFields` (`_x_reason`, `_x_mode`, `_x_sonic`, `_x_app_name`) that Slack's own web client tags every "xhr" with for telemetry - **not required for correctness** but present on every real client request.

`client.counts` response (`ClientCountsResponse`): `channels`, `mpims`, `ims` - each a `ChannelSnapshot{id, last_read, latest, history_invalid, mention_count, has_unreads}` - plus wee-slack's typing adds `threads{has_unreads, mention_count}`, `channel_badges{channels, dms, app_dms, thread_mentions, thread_unreads}`, and `saved{uncompleted_count, uncompleted_overdue_count}` (confirms a "Saved items" badge count exists server-side, see §8).

**UNCERTAIN**: `users.channelSections.list` (sidebar custom sections and ordering) was **not found** in either wee-slack or slackdump - neither client appears to replicate Slack's custom-sidebar-sections feature. Treat its existence/shape as asserted by the task description only; capture it from a live DevTools session before implementing.

## 4. Real-time transport

**CONFIRMED - two live paths, chosen by token type.** wee-slack's `slack_workspace.py:_connect()` branches on `self.token_type`:

```python
if self.token_type == "session":      # xoxc- token path
    team_info = await self.api.fetch_team_info()      # team.info
    ...
    await self._connect_ws(
        f"wss://wss-primary.slack.com/?token={api_token}&gateway_server={team_id}-1"
        f"&slack_client=desktop&batch_presence_aware=1"
    )
else:                                    # classic bot/user OAuth token path
    rtm_connect = await self.api.fetch_rtm_connect()   # rtm.connect
    await self._connect_ws(rtm_connect["url"])
```
`token_type` is derived from the token prefix: `startswith("xoxc-")` → `"session"` (`slack_workspace.py:271`). So: **`rtm.connect` still works for classic tokens, but an xoxc client/session token connects directly to `wss://wss-primary.slack.com/` with the token and a `gateway_server=<teamID>-1` query param** - this is the real desktop client's "flannel" gateway endpoint, confirmed live in wee-slack's own code, not inferred. The `d` cookie is sent as the WebSocket's `Cookie` header during the handshake (`create_connection(url, ..., cookie=get_cookies(self.config.api_cookies.value), ...)`).

**Reconnect**: a `reconnect_url` event type delivers a fresh URL to reconnect to without replaying the whole boot sequence (`data["type"] == "reconnect_url"` → stored, reused on next `_connect()` before falling back to `rtm.connect`/gateway URL). On socket error, `reconnect()` is scheduled. The initial `hello` frame carries `fast_reconnect: bool` - if true and not the very first connect, the client skips re-running `_initialize()` and `_load_unread_conversations()`.

**Ping/keepalive**: client-initiated `{"type": "ping"}` sent on a timer only if time since last received frame hasn't already exceeded the configured network timeout (otherwise it reconnects instead of pinging) (`slack_workspace.py:ping()`). A separate `{"type": "tickle"}` message is sent at most every 20s (`tickle()`, driven by a 20000ms weechat timer in `register.py`) - this appears to be a presence/activity heartbeat distinct from the ping/pong keepalive, confirmed in code but its exact server-side semantics are **UNCERTAIN**.

**Event types confirmed observed/handled** in `slack_workspace.py:ws_recv()`: `hello`, `error` (code `1` = "socket URL has expired"), `reconnect_url`, `pref_change`, `user_status_changed`, `user_invalidated`, `subteam_created/updated/members_changed/self_added/self_removed`, `emoji_changed` (subtypes `add`/`remove`), `channel_joined`/`group_joined`/`channel_rename`/`group_rename`, `message` (subtypes `message_changed`, `message_deleted`, `message_replied`, `channel_topic` as a sub-subtype), `im_close`/`mpim_close`/`group_close`/`channel_left`/`group_left`, `reaction_added`/`reaction_removed` (each checked for `item.type == "message"`), `channel_marked`/`group_marked`/`mpim_marked`/`im_marked`, `thread_marked`, `thread_subscribed`/`thread_unsubscribed`, `sh_room_join`/`sh_room_update` (Slack huddles), `user_typing`. **`thread_broadcast`, `update_thread_state`, and `badge` events named in the task were not found as distinct handled types in this code** - reply-broadcast is instead carried as a flag/subtype on the `message`/`message_replied` event (`reply_broadcast` param on the send side, see §5), and badge counts arrive by re-polling `client.counts`/websocket `channel_marked` family rather than a separate `badge` event in this client. Mark those three as **UNCERTAIN** for exact wire shape.

## 5. Messages

**CONFIRMED**, all against the workspace-scoped `/api/` base (§1), form-encoded:

- `conversations.history` - params: `channel`; wee-slack also calls it with `oldest`/`inclusive` for incremental fetch after `last_read`.
- `conversations.replies` - also implemented server-side (`internal/edge/canvas.go` in slackdump re-uses it) with `channel`+`ts` of the parent.
- `conversations.mark` - params: `channel`, `ts`.
- `subscriptions.thread.mark` - params: `channel`, `thread_ts`, `ts`, `read=1` - this is the thread-specific read-marker, separate from `conversations.mark`.
- `chat.postMessage` - wee-slack sends **plain `text`** with `as_user: True`, `link_names: True`, plus `thread_ts`/`reply_broadcast` for thread replies. **No `blocks`/`rich_text` payload was found being sent by wee-slack** - it is a plain-text IRC-bridge client, so this is not proof the real desktop client never sends blocks. The real Slack web client is known (general knowledge, **UNCERTAIN** here since not seen in these sources) to compose messages as a `rich_text` block (Slack's internal "Slate" document → `blocks: [{type: "rich_text", elements: [...]}]`) rather than a bare `text` string, falling back to `text` only for simple cases/bots.
- `chat.update` - params: `channel`, `ts`, `text`, `as_user`, `link_names`.
- `chat.delete` - params: `channel`, `ts`, `as_user`.
- `reactions.add` / `reactions.remove` - params: `channel`, `timestamp`, `name`.
- `emoji.list` - no params beyond token/cookie.
- `users.info` - batchable only via the **edge API**, not the public one: slackdump's `GetUsers()` (`internal/edge/userlist.go`) loops calling the edge `users/info` with an `UpdatedIDS` map of `{userID: lastSeenUpdatedTimestamp}`; the response can return `PendingIDS` for IDs not yet resolved, requiring the caller to loop again until `PendingIDS` is empty - this pending/poll pattern is specific to the internal edge `users/info`, not the public `users.info`.
- `users.list` equivalent for channel membership is the edge `users/list` (`UsersListRequest{Channels, Filter, Marker, Count}` → cursor via `NextMarker`, not the public API's `cursor`/`limit`).

## 6. Activity feed

**UNCERTAIN.** `activity.feed` was **not found** in wee-slack or slackdump - neither project implements an activity/notifications feed. No endpoint name, params, or response shape could be confirmed from source. Treat the task's `types=thread_reply,message_reaction,at_user,...` / `mode` / `limit` params as unverified; capture a live request from the web client's Activity tab via DevTools before implementing.

## 7. Threads view

**UNCERTAIN.** `subscriptions.thread.getView` was **not found** in either codebase. The only thread-subscription-adjacent endpoint confirmed is `subscriptions.thread.mark` (§5), which marks read state on a single thread, not a sidebar "Threads" view listing. No evidence either way on this endpoint's existence beyond the task's own description.

## 8. Later / saved

**Partially confirmed.** No `saved.list`/`saved.add` calls exist in either reviewed client. However, `client.userBoot`'s response type carries a `Starred []any` field and `client.counts`'s response carries `saved{uncompleted_count, uncompleted_overdue_count}` (both confirmed in slackdump's and wee-slack's typed responses, §3) - this proves server-side "Saved items" and "stars" are both still live concepts with counts surfaced through `client.counts`, consistent with the task's claim that `stars.*` is the older equivalent of `saved.*`. The `saved.list`/`saved.add` method names themselves are **UNCERTAIN**, unconfirmed in source.

## 9. Search

**Partially confirmed.** slackdump implements `search.modules.channels` against the **webclient API** (not edge) - `internal/edge/search.go`, `const ep = "search.modules.channels"` - with a rich form including `module: "channels"`, `query`, `page`, `client_req_id`, `browse_id` (random UUIDs), `cursor: "*"`, `filter` (channel type), `sort`/`sort_dir`, `search_context: "desktop_channel_browser"`, and the standard `WebClientFields` telemetry block. This confirms `search.modules` is a real, current internal endpoint family (`search.modules.<module>`, e.g. `channels`, and by extension likely `messages`/`people`) used by the real desktop client's in-app search/browse UI - but only the `channels` module's exact param set was seen; `search.modules.messages`'s params and the `in:`/`from:`/`has:link`/`is:thread` filter-string grammar are **UNCERTAIN** from this source (that filter syntax is documented publicly by Slack for the search *UI*, not confirmed here as literal API params - it's most likely passed through as part of the free-text `query` string rather than as separate structured fields, based on how `query` is used for `search.modules.channels`). The public `search.messages` Web API method was not exercised by either project.

## 10. Drafts

**UNCERTAIN.** `drafts.list` was **not found** in either codebase.

## 11. Block Kit interactivity from a user client

**Mostly UNCERTAIN - flagged explicitly as the task warned.** Neither wee-slack (a text-only IRC bridge with no block-button rendering) nor slackdump (an exporter, not an interactive client) implements clicking a Block Kit button, so **no source evidence exists in either reviewed project** for this item. `slack/block_action.go` (`rusq/slack`) only defines the outbound **message construction** types (`ActionBlock`, `BlockElements`) used when *building/posting* a message that contains interactive elements - it does not show the *client-side click* request. What can be said from general public knowledge of Slack's Block Kit model (clearly marked UNCERTAIN, not sourced from the reviewed repos):
- A button click is understood to fire a `block_actions` interaction payload containing `action_ts`, `container` (channel/message reference), and the acting block's `action_id`/`value` - publicly documented for *apps* receiving interactivity webhooks, but whether/how a **first-party user session** (xoxc+cookie) triggers the equivalent client→server call, and whether it's literally `chat.attachmentAction` (the legacy pre-Block-Kit mechanism name) or a newer `blocks.actions`-style endpoint, was not confirmed in any source reviewed.
- Modals: whether `views.submit` vs `views.update` is called, and whether `view_submission`/`view_opened`/`view_pushed` arrive over the websocket RTM-style stream or only in the HTTP response to the triggering call, is **entirely unconfirmed** here.

If this is load-bearing for the client, the only reliable way to pin it down is a live DevTools Network capture against a real workspace clicking a real interactive message - do that before implementing rather than trusting any secondhand description, including this one.

## 12. Rate limits and pitfalls

**Partially confirmed.** slackdump's edge `do()` handles `429 Too Many Requests` by reading `Retry-After`, sleeping once, retrying once, and surfacing `slack.RateLimitedError{RetryAfter}` if still rate-limited after that single retry (`internal/edge/edge.go:do`) - i.e. the real client-side convention is "respect `Retry-After`, don't hammer." Known pitfalls, confirmed or strongly implied by the surrounding auth code:
- **Token/cookie pairing is strict**: `auth.NewValueAuth` refuses to construct a session (`xoxc-`) provider without a cookie (`ErrNoCookies`), and the cookie's expected name is literally `d` (`auth/value.go`).
- **Logout invalidates both**: not directly shown in these diffs, but implied by the whole cookie-refresh dance in `auth/token.go` (`getTokenByCookie` re-derives a token from a still-valid `d` cookie via `/ssb/redirect`) - if the cookie itself is dead (user logged out, session revoked by admin), both the token and this refresh path die together. **UNCERTAIN** beyond that inference.
- **Enterprise Grid** changes the edge API's path segment from team ID to **enterprise ID** (`edge.go:NewWithInfo` uses `info.TeamID` directly for the edge cache path, while `NewWithClient` takes an explicit `teamID` argument the caller must supply correctly - getting grid-vs-single-workspace ID selection wrong breaks `edgeapi.slack.com/cache/<id>/...` silently"). wee-slack separately special-cases `team_is_org_level` when deciding whether `enterprise_id` is itself the workspace ID (`slack_workspace.py:_connect`).
- **Bans**: no explicit ban-detection/backoff logic beyond the single 429 retry was found in either project; aggressive internal-API use (e.g. `search.modules`, batched `users/info` polling loops) is exactly the kind of traffic pattern real Slack clients don't produce at API-tool volumes, and is the likeliest trigger for anti-automation detection. Treat this risk as real but **UNCERTAIN** in specifics (no documented threshold found).

---

### Primary sources
- wee-slack `slack/` package: [slack_api.py](https://github.com/wee-slack/wee-slack/blob/main/slack/slack_api.py), [slack_workspace.py](https://github.com/wee-slack/wee-slack/blob/main/slack/slack_workspace.py), [util.py](https://github.com/wee-slack/wee-slack/blob/main/slack/util.py), [extract_token_from_browser.py](https://github.com/wee-slack/wee-slack/blob/main/extract_token_from_browser.py), `typings/slack_api/*.pyi`
- slackdump: [internal/edge/edge.go](https://github.com/rusq/slackdump/blob/master/internal/edge/edge.go), [client.go](https://github.com/rusq/slackdump/blob/master/internal/edge/client.go), [client_boot.go](https://github.com/rusq/slackdump/blob/master/internal/edge/client_boot.go), [search.go](https://github.com/rusq/slackdump/blob/master/internal/edge/search.go), [userlist.go](https://github.com/rusq/slackdump/blob/master/internal/edge/userlist.go), [auth/value.go](https://github.com/rusq/slackdump/blob/master/auth/value.go), [auth/token.go](https://github.com/rusq/slackdump/blob/master/auth/token.go), [auth/rod.go](https://github.com/rusq/slackdump/blob/master/auth/rod.go), [doc/login-manual.md](https://github.com/rusq/slackdump/blob/master/doc/login-manual.md)
- rusq/slack: [block_action.go](https://github.com/rusq/slack/blob/master/block_action.go)
