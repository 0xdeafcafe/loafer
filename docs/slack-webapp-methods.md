# Slack web client API methods (recovered from the desktop app's cached webapp JS)

Source: `Service Worker/CacheStorage/` of the Mac App Store Slack install (plain, uncompressed minified JS chunks;
~340 files, the biggest being the 30MB `gantry-v2` bundles). Files containing `xox*-` strings were skipped and never read.
`Cache/Cache_Data` and `app.asar` were not needed. No network calls made. Snippets are trimmed and minified; variable names are bundle-local.

Date of capture: 2026-10-08 (webapp build is whatever Slack last cached, so treat param lists as "current as observed").

## 0. Transport conventions (important for loafer)

- FOUND. Every method is registered as a "fetcher" by name: `(0,N.r)("usersChannelSectionsListFetcher","users.channelSections.list")`.
  ~1145 method names are registered this way; full list is in the "Other methods" section (selected).
- FOUND. Transport is the classic form POST to `/api/<method>` on the workspace host, multipart `FormData`, with `token` in the body.
  Unauthed helper shows the shape:
  `a=\`/api/${e}?_x_id=noversion-${r}\`,n.open("POST",a,!0);let s=new FormData ... s.append(e,t[e])`
  Extra query params seen: `_x_id`, `_x_version_ts`; extra body args: `_x_reason` (the `reason` field every call carries).
- FOUND. Fetchers have `camelCaseInput` (default on: JS camelCase args become snake_case on the wire) and optional `camelCaseOutput`:
  `(0,h.r)("draftsListActiveFetcher","drafts.listActive",{camelCaseOutput:!0})`. Some use `{camelCaseInput:!1}`.
  Object/array args are `JSON.stringify`-ed into a form field (blocks, destinations, container, actions, state, file_ids).
- NOT FOUND (as an active call path). edgeapi JSON. The only hit is an allow-list entry `"edgeapi.slack.com"` in the resource-timing
  host list. No `/cache/...` edge routes, no JSON POST helper. Treat everything as `/api/` form POST.
- FOUND. Beacon variant for unload: `(0,r.A)({apiUrl:"/api/",method:n,token:a,args:i,versionTs:e,versionUid:t})`.

## 1. Sidebar sections

FOUND methods (registry): `users.channelSections.list`, `.set`, `.create`, `.delete`, `.channels.bulkUpdate`, `.channels.remove`,
`.entities.update`, `.createLink`, `.linkInfo`, `.validateShare`.

```
let R="users.channelSections.list";(0,N.r)("usersChannelSectionsListFetcher",R)
```

Args for `.list` are NOT FOUND (call site goes through an opaque module alias). Boot payload carries sections in
`data.sections.channel_sections` (`i&&t.sections?.channel_sections!=null`), so list is probably a refresh of that.
Create thunk args (client side): `{name, emoji, channelIds, nextChannelSectionId, sharedChannelSectionArgs}`; response read:
`n.channel_section_id`, `n.errors`. Section types enum includes Standard, Stars, DirectMessages, SlackConnect, UserGroup.
Bulk mark-read of a section passes `channel_section_id` (analytics only).

RTM events FOUND:
```
d1("channel_section_upserted",(e,t,a)=>{let n=e.channel_section_type; ... tp.t({...e,type:e.channel_section_type})
d1("channel_section_deleted",({channel_section_id:e}={},t)=>...
d1("channel_sections_channels_upserted",(e,t)=>{let{channel_section_id:a,channel_ids:n,sort_source:i}=e
d1("channel_sections_channels_removed",({channel_section_id:e,channel_ids:t}={},a)=>...
d1("channel_section_reset" ...   d1("channel_sections_entities_upserted"/"_removed" (entities[], channel_section_id)
```
User-group sections carry `usergroup_id`.

## 2. Activity

FOUND methods: `activity.feed`, `activity.feed.scoreEntries`, `activity.markRead`, `activity.markUnread`, `activity.markAllRead`,
`activity.archive`, `activity.unarchive`, `activity.clearAll`, `activity.clearBefore`, `activity.prefs.update`,
`activity.views`, `.views.create/update/delete/hide/unhide`.

activity.feed args (camelCase, snake_case on wire):
```
ex={limit:i,reason:"fetchActivityFeed",types:ek,mode:eP,cursor:R?void 0:eb,archiveOnly:ew,channelIds:..,channelSectionIds:..,
unreadOnly:eR,priorityOnly:eN,onlySalesforceChannels:eM,excludeAutomations:eD,automationsOnly:eL,isActivityInbox:ee,
...eO&&{priority_scoring_method:eF}}   // e((0,c.X_)(ex))
```
Default call: `{limit:20,mode:"chrono_reads_and_unreads",filter:All}`. Response read: `items[]`, `response_metadata.next_cursor`, `request_id`;
item = `{feed_ts,is_unread,item:{type,...},key}`.
Modes FOUND: `chrono_reads_and_unreads`, `chrono_unreads`, `chrono_v1`, `unreads_first_v1`, `priority_unreads_first_v1`,
`priority_unreads_v1`, `priority_reads_and_unreads_v1`, `chrono_priority_only_unreads_v1`, `chrono_priority_only_reads_and_unreads_v1`,
`chrono_threads_unreads_v2`, `chrono_threads_reads_and_unreads_v2`, `priority_unreads_inbox_v1`.
Types mapping FOUND:
```
mentions:["at_user","at_channel"...],mention_at_user:["at_user"],mention_at_channel:["at_channel","at_everyone"],mention_at_user_group:["at_user_group"],
mention_keyword:["keyword"],replies:["thread_v2"],reactions:["message_reaction"],apps:["bot_dm_bundle"],dms:["dm"],
invitations:["internal_channel_invite","external_channel_invite","external_dm_invite"],channel:["channel"]
```
plus `saved_reminder`, `list_record_edited`, `team_joiner_invite`, `quietly_added_to_channel`, `prejoin_dm_welcome_party_alert`.
Empty filter = all of the above flattened.

markRead: `method:"activity.markRead",args:o` where `o` is the `activityItem` minus `reason`, snake_cased (item has
`{channelId,messageTs,ts,feedTs,threadTs,key,type}` shape from the saved-for-later call site). Exact wire keys NOT FOUND.
markAllRead used as `x1({channelIds:s,reason})`, returns `{ok,items}`.
activity.views.create filters: `{entry_types, channel_section_ids, archive_only, unread_only, priority_only, channel_ids, only_salesforce_channels, exclude_automations, automations_only}`
with `{name,emoji,sort,density}`.
Unread counts: `client.counts` returns `activity_v2` and the boot payload has `activity_inbox_badge_counts:{total_unread_count,activity_v2}`.
RTM: `activity`, `activity_clear_all_completed`, `activity_views_updated`.

What loafer sends and reads, all UNVERIFIED until a DevTools capture (`internal/slack/tabs.go`):
- `activity.feed` with `limit=30`, `mode=chrono_reads_and_unreads`, `types` comma-joined (FormData joins an array that way).
- Inside `item`: `message{ts,channel,thread_ts,author_user_id,user,text}`, `reaction{user,name}`, and for bundles
  `bundle_info.payload.thread_entry{channel_id,thread_ts,latest_ts}` or `.payload.message`. When there's no text, the message is fetched
  with `conversations.history` (`oldest=latest=ts`, `inclusive`) or `conversations.replies`.
- `activity.markRead` with `key, feed_ts, type, channel_id, message_ts`; errors are only logged.
- `activity_v2` in `client.counts`: read as a number, or an object's `unread_count`/`total_unread_count`/`badge_count`/`count`.
- The RTM events' bodies aren't read: any of them fetches the feed again, once it's been opened.

## 3. Threads view

FOUND methods: `subscriptions.thread.getView`, `.mark`, `.get`, `.add`, `.remove`, `.clearAll`, `.getTimestamps`.
```
e((0,r.KZ)({currentTs:a,limit:c,reason:_,priorityMode:g,channelId:f})).then(({threads:a,has_more:n,total_unread_replies:i,threads_state:r})=>
```
Wire args: `current_ts`, `limit`, `priority_mode`, `channel_id` (inferred from camelCase->snake).
Response: `threads[]` each `{root_msg (needs ts, thread_ts, channel, subscribed), unread_replies[], latest_replies[]}`,
`has_more`, `total_unread_replies`, `threads_state:{has_unreads,mention_count,mention_count_by_channel,unread_count_by_channel,timestamp}`.
mark: client-side call is `{channelId, threadTs, ts, markUnread, reason}` (wire: `channel`, `thread_ts`, `ts`, `read` or similar; exact keys NOT FOUND).
RTM events FOUND: `thread_marked` (`e.subscription:{channel,thread_ts,last_read}`), `thread_subscribed`, `update_thread_state`, `update_global_thread_state`
(`has_unreads, unread_count_by_channel, timestamp, channel_badges, mention_count, mention_count_by_channel, org_wide`).

## 4. Later / Saved

FOUND methods: `saved.list`, `saved.add`, `saved.update`, `saved.delete`, `saved.get`, `saved.markCompleted`, `saved.clearCompleted`,
`saved.bulkUpdate`, `saved.bulkDelete`.
```
savedItemToDeleteArgs(e){let t={item_type:e.itemType,item_id:e.itemId};switch(e.itemType){case message:return{...t,ts:e.ts}; list_record:{ts:recordId}; list_record_field:{ts:recordId,item_detail:columnId}; canvas_section:{item_detail:sectionId}; file/reminder:t}
{method:"saved.bulkDelete",args:{delete_all:!0,filter:n.filter}}
```
saved.add (message): `{channelId,messageTs,dateDue,todoState}` => `item_type, item_id(channel), ts, date_due, todo_state`.
saved.update: `{itemType,itemId,ts|itemDetail,todoState,dateDue,mark}` where `mark` is `uncompleted`/`unarchived` (and presumably `completed`/`archived`).
Item schema (response validator, FOUND):
```
{is_archived:boolean,date_due:number|null,date_completed:number|null,state:enum["archived","completed","in_progress"],
 todo_state:enum["completed","saved","to_do"]|null}
```
Filters (list/bulk): `saved`, `todo`, `completed`, `archived`(inferred), `todo_overdue`, `todo_overdue_all`, `todo_deleted_messages`(inferred), `none`.
List call: `fetchAndSyncSavedList({filter, laterTodos:true, replaceFilter})`; limit/cursor keys NOT FOUND (max page likely 30).
RTM: `saved_added` (`e.saved`, `e.client_id`), `saved_updated`, `saved_deleted`, `saved_clear`, `saved_due`.
Counts: `client.counts` response has a `saved` object.

What loafer sends and reads, all UNVERIFIED until a DevTools capture (`internal/slack/tabs.go`):
- `saved.list` with `filter=saved`, `limit=50`, one page; the list is read from `saved_items`, else `items`. Anything archived, completed or
  not `in_progress` is dropped. A `message` in an item is used if it's there; else the message is fetched (a saved reply isn't found that way).
- Done: `saved.update` with `item_type, item_id, ts, mark=completed` (`completed` guessed as the pair of `uncompleted`).
- Remove: `saved.delete` with `item_type, item_id, ts` (FOUND, above).
- The `saved_*` RTM events' bodies aren't read: any of them fetches the list again, once it's been opened.

The message actions menu (`internal/slack/actions.go`), also UNVERIFIED:
- Save: `saved.add` with `item_type=message, item_id=<channel>, ts` (the found shape above, `todo_state` left out). A duplicate is guessed to answer an
  `already_*` code (`already_saved`), which loafer takes as done.
- Remind me: the same `saved.add` with `date_due` (unix seconds, a guess; `saved.list`'s `date_due` is read the same way). Picked over `reminders.add`
  because the web client's "remind me about this" is a saved item with a due time now (`saved_reminder` / `saved_due` are its events), and Later already
  draws due times. Already saved, it becomes `saved.update` with the same keys and the new `date_due`.
- Pin: `pins.add` / `pins.remove` with `channel, timestamp` (documented Web API). `already_pinned` and `no_pin` are taken as done. A pinned message carries
  `pinned_to: [channel ids]` in history (documented). `pin_added` / `pin_removed` events carry `channel_id` and `item.message.ts`; read defensively.
- Mark unread: `conversations.mark` with the `ts` of the message before (the web client's way), or a microsecond before if that isn't held. Not used
  for threads, whose `subscriptions.thread.mark` keys are still unconfirmed.

## 5. Drafts

FOUND methods: `drafts.list`, `drafts.listActive`, `drafts.create`, `drafts.update`, `drafts.delete`, `drafts.bulkDelete`, `drafts.info`.
```
drafts.create args:{blocks:JSON,client_msg_id,attachments,destinations:JSON,file_ids:JSON,unfurl,is_from_composer,date_scheduled,date_stashed,file_permissions}
drafts.update args:{blocks:JSON,client_last_updated_ts,attachments,destinations:JSON,draft_id,file_ids:JSON,unfurl,date_scheduled,date_stashed,is_from_composer,file_permissions}
drafts.list   args:{is_active,limit,last_updated_ts?,next_ts?}   -> {drafts,files,has_more}
drafts.listActive (camelCaseOutput) -> {activeDraftIds,hasMore,nextTs}   // args {limit,nextTs}
drafts.bulkDelete args:{draft_ids?,client_last_updated_ts:Date.now().toString(),is_select_all}
```
The UI deletes via `drafts.bulkDelete` (plain `drafts.delete` registered but no call site found; args assumed `draft_id`, `client_last_updated_ts`).
`destinations` = JSON array of `{channel_id, thread_ts, broadcast}`-style objects (shape not captured). RTM: `draft_create`, `draft_update`, `draft_delete`, `draft_send`.

## 6. Block Kit interactivity

FOUND `blocks.actions`:
```
M={service_id:s,app_id:l,service_team_id:y,actions:JSON.stringify(h),container:JSON.stringify(f),client_token:T,function_execution_id:A};
j&&(M.state=JSON.stringify({values:j}))   // method "blocks.actions", reason "dispatch_action_to_developer"
```
`client_token` comes from a pending-token registry (`generateAndEnqueue(state,{windowId})`), format elsewhere `web-${Date.now()}`.
`action_ts`: NOT FOUND in the JS (no literal); it is inside each `actions[]` entry if anywhere. `container` examples: message
`{type:"message",channel_id,message_ts}`, view `{type:"view",view_id}` (container key = `${channelId}-${messageTs}` or `view_id`).
Hardcoded handoff example: `{method:"blocks.actions",args:{service_id:"BSLACKBOT",container:n,actions:i,client_token:\`web-${Date.now()}\`}}`.
Also `blocks.suggestions` (external select): `{service_id,app_id?,service_team_id,function_execution_id,container:JSON,action_id,block_id,value}` -> `{option_groups|options}`.
Also `blocks.format`, `blocks.page.getContent`.

Modals: FOUND method names `views.submit`, `views.close`, `views.get` (registered fetchers; call args NOT FOUND).
Modal delivery: via RTM, correlated by client_token:
```
function viewOpened({view:e,view_id:t,view_type:a,previous_view_id:r,client_token:p,timeout_range:g,title:v,submit:b,close:C,app_id:k,channel_id:S,trace_id:x},w,T)
 ... let e=c.A.check(I,p);if(!e)return; ... d1("view_opened",viewOpened),d1("view_updated",viewUpdated)
```
So: the click sends `blocks.actions` with a `client_token`; the app calls `views.open` server-side; Slack pushes RTM `view_opened`
carrying the same `client_token`, `view`, `view_id`, `view_type` (`modal`, `home`, ...), `previous_view_id` (for push/stack), `timeout_range`.
`view_updated {view_id, app_id, view}`. Edit-hash container key is `${viewId}_${viewHash}`. Home tab: `view_type==="home"` with `channel_id`.

What loafer sends and reads (`internal/slack/blocks.go`, `internal/store/modal.go`), none of it checked against a DevTools capture:
- `blocks.actions` with `service_id` (the message's `bot_id`), `app_id` (the message's `app_id`, when it has one), `service_team_id`,
  `client_token` (`web-<ms>-<n>`), `container` `{type:"message",channel_id,message_ts}` and `actions` a one-item array. That item is a GUESS from
  the public `block_actions` payload: `{type, action_id, block_id, text, value, action_ts}`, plus `selected_option {text,value}` for a static
  select or overflow and `selected_date` for a datepicker. No `state`, no `function_execution_id`. Thread messages send the same container.
- `view_opened` is taken only when its `client_token` is one loafer sent lately (the last 8), or its `previous_view_id` is a view it holds;
  `view_type` (or `view.type`) must be `modal`. `view` is read as an object, or as a string of JSON if it comes that way (UNCERTAIN).
  `view_pushed` (GUESSED, by analogy) stacks like it, `view_updated` replaces by `view_id`, `view_closed` (GUESSED) drops that view and those on it.
- `views.submit` (args NOT FOUND, GUESSED): `view_id`, `view_hash` (the view's `hash`), `client_token`, and `state` as `{"values":{block_id:
  {action_id:{type, value | selected_option | selected_options | selected_date}}}}`, empty ones as `null`. Read back, also GUESSED: `response_action`
  and `errors` (block_id to message), as an app answers `view_submission`. Errors show under their inputs; `update` and `push` wait for the socket;
  anything else closes the modal (`clear` only closes the top one).
- `views.close` (args NOT FOUND, GUESSED): `view_id`, `client_token`. The modal goes at once; a failure is only flashed.

## 7. Search

FOUND methods: `search.modules.ai/channels/external/files/topResults/workObjects`, `search.inline`, `search.team`, `search.enterprise`,
`search.autocomplete`, `.autocomplete.tags/triggers/topEmojis/nlTypeahead/topEngagedFiles`, `search.precache`, `search.save`, `search.delete`, `search.feedback`.
`search.modules.messages` / `.people` registrations NOT FOUND in this cache (UI still has Qt.MESSAGES/PEOPLE modules), so the messages module is
probably the plain `search.modules` with `module:"messages"`; unconfirmed.
Files module call: `iQ({query:"type:quip creator:U123",module:"files",reason,page:1,count:1})` -> `{pagination,total_count,...}`.
search.inline: `{search_session_id,client_req_id,max_ts,min_ts,channel,user,count,page,query,thread_replies,extract_len,recent_channels,from_me,with_me}`.
Channel browse search: `{search_channel_types:["exclude_archived","org_wide"],sort,sort_dir,limit:20,query,cursor,team_ids}`.
Filter syntax lives in the query string: `from:`, `in:`, `type:`, `creator:`, `is:thread`, plus `before:/after:` (the `is:thread` toggle exists; others by convention).

What loafer uses (UNTESTED against a live workspace): the public `search.messages` with `query`, `page` (from 1), `count=20`,
`highlight=true`, which takes a session token. Read: `messages.total`, `messages.matches[]` (`channel{id,name,is_im,is_mpim}`, `user`,
`username`, `ts`, `text`, `permalink`), and the page from `messages.paging{page,pages}` or, failing that, `messages.pagination{page,page_count}`.
Assumed: highlight marks are U+E000 / U+E001 round each match (the UI lights the query's words itself when they're absent); a reply's
parent is `thread_ts` on the match or, when that's missing, the permalink's `?thread_ts=`; `in:<#C123>` narrows to a conversation
(unchecked for DMs, where the UI may need `in:<@U123>`). `cursor` paging (`*`, then `next_cursor`) is documented too but not used.
Going to a result outside the held window: `conversations.history` with `latest=<ts>&inclusive=true&limit=50` for what's before it,
then `oldest=<ts>&inclusive=true&limit=50`; Slack fills a page from the newest end, so that second page is only kept when `has_more` is false.

## 8. Boot, counts, DMs, websocket

FOUND: `client.userBoot`, `client.channels`, `client.dms`, `client.extras`, `client.counts`, `client.init`, `client.gantryBoot`,
`client.shouldReload`, `client.getWebSocketURL`, `conversations.view`, `conversations.mark`, `users.prefs.get`, `team.prefs.get`.
"Split boot": `refreshClientData` runs `client.channels` + `client.extras` in parallel with the main boot (`client.userBoot`/`client.init`). Boot args:
```
o={_x_reason:a,version_all_channels:V(),...r?{min_channel_updated:r}:{},...extra}   // method "client.userBoot"
```
client.dms / channels / extras arg lists: NOT FOUND (the wrapper module was not in the cache).
client.counts args (FOUND):
```
{thread_counts_by_channel:!0,org_wide_aware:!0,include_file_channels:!0,include_all_unreads:<flag>,channel_ids:s}
 + counts_last_fetched (or dry_run_last_fetched)
```
Response: `{channels,ims,mpims,threads,alerts,file_channels,channel_badges,counts_last_fetched,activity_v2,saved}`.
Initial message pane: `conversations.view` args `{canonical_avatars,no_user_profile,ignore_replies,no_self,include_full_users,include_use_case,include_stories,no_members,include_mutation_timestamps,count:28,include_free_team_extra_messages,channel}`.
IM/MPIM lists (older): `{get_latest:true,get_read_state:true,limit}` / `{get_latest:true}`.

WebSocket (FOUND):
```
socketUrl: fetchEndpoint({method:"client.getWebSocketURL",teamId}) -> {url:e.primary_websocket_url, fallbackUrl:e.fallback_websocket_url, ttlSeconds:e.ttl_seconds, context:e.routing_context}
```
URL built by appending query params: `token`, `sync_desync=1`, `slack_client=desktop`, `start_args=<urlencoded>`, `no_query_on_subscribe=1`,
`flannel=3`, `lazy_channels=1`, `gateway_server=<routing_context>`, `enterprise_id`, `exclude_events`, `ws_region`, `is_backup=1`, `last_primary_region`,
`frt` (fast reconnect token), `batch_presence_aware=1`, `slack_route`, and `api_url` on dev.
`start_args` = `{agent, agent_version, eac_cache_ts:true, cache_ts:0, name_tagging:true, only_self_subteams:true, connect_only:true, ms_latest:true, org_wide_aware:true, no_presence?, no_subteams?, feature_*}`.
Separate voice/dictation socket: `wss://wss-primary.slack.com/switchboard?token=..&team=..` (dev: `wss-primary.dev.slack.com`).
Client frames sent over WS: `{type:"presence_sub",ids:[...]}` (debounced 100ms).
Flannel: NOT used as HTTP `users/info` or `users/list` in this cache (`flannelOverHttp` is stubbed "Not yet implemented"). Flannel queries are WS requests
via a helper `P(teamId,{method:J8.<ENUM>,args})` (e.g. `EXTERNAL_TEAMS_COUNTS` with `{team_ids,count,marker}`); the enum values were not captured. Member lookups:
`query={queries,count,fuzz:1,filter:"NOT deactivated",match_email,top_users,current_channel,current_thread_users,default_workspace}` -> `{objects,next_marker}`.

## 9. conversations.history / replies extras

Fetchers FOUND (`conversations.history`, `.replies`, `.historyChanges`, `.view`, `.mark`, `.info`, `.list`, `.listPrefs`); the extra-param call site
is not in this cache except the boot one above (`ignore_replies:!0`, `include_stories`, `include_mutation_timestamps`, `no_members`, `canonical_avatars`).
`include_pin_count`: NOT FOUND. Replies thunk uses `{channelId,threadTs,oldest,latest,limit}` and pages both directions, returning `{msgs,hasMore,deleted}`.

What loafer sends for threads (`internal/slack/thread.go`), none of it yet checked against a DevTools capture:
- `conversations.replies` with the public API's `channel`, `ts`, `limit=200` and `cursor` from `response_metadata.next_cursor`. Whether a later page leads with the parent again is UNCERTAIN, so it's dropped if it does. It stops at 1000 messages.
- replies go by `chat.postMessage` with `thread_ts`, and `reply_broadcast=true` for "also send to the channel". That the returned message carries `subtype: thread_broadcast` is assumed; if it doesn't, the channel's copy comes down the websocket instead.
- `subscriptions.thread.mark` with `channel`, `thread_ts`, `ts`, `read=1`: wee-slack's keys (slack-internal-api.md §5), not the web client's, whose wire keys are NOT FOUND (§3 above). A failure is only logged.

Message shapes the renderer reads (`internal/ui/blocks.go`, `internal/mrkdwn/richtext.go`), from Slack's public Block Kit docs, not from a trace:
- `blocks[]`, each decoded on its own so one unknown block costs only itself. `rich_text` parts: `rich_text_section/list/preformatted/quote`;
  inline `text` (with `style.bold/italic/strike/code`), `link`, `user`, `channel`, `usergroup`, `broadcast`, `emoji` (with `unicode`), `date`, `color`.
- UNCERTAIN: `image_width`/`image_height` on stored image blocks and attachments; used when present.
- UNCERTAIN: an attachment's `ts` is a number on legacy attachments and a string on message unfurls; read as either.
- `files[]`: `name`, `title`, `mimetype`, `pretty_type`, `size`, `permalink`, `mode` (`tombstone` and `hidden_by_limit` carry no name).

## Managing the sidebar (`internal/slack/manage.go`; UNVERIFIED, nothing here is tried against a live workspace)

The documented ones, which take a session token, and what loafer reads:
- Browse: `conversations.list` with `types=public_channel`, `exclude_archived=true`, `limit=200` and `cursor` from `response_metadata.next_cursor`; each channel's `name`, `is_member`, `num_members`, `purpose.value`. loafer fetches up to 25 pages and filters by name itself. The web client's own browse is `search.modules.channels` with `{search_channel_types:["exclude_archived","org_wide"],sort,sort_dir,limit:20,query,cursor,team_ids}` (§7), but its response isn't known, so it isn't used.
- Read-only preview of a channel you're not in: `conversations.history` as for any channel. UNCERTAIN whether Slack lets a session token read every public channel's history; if not, the preview shows an error and `join` still works.
- `conversations.join` with `channel` (answers `channel`), `conversations.leave` with `channel`, `conversations.close` with `channel`.
- `conversations.open` with `users` (comma separated) and `return_im=true`, answering `channel{id,...}`. A one-person open is a DM, several a group DM. If the channel comes back without `is_im`/`is_mpim` or a name, loafer fills them in from who it asked for.

The web client's, with guessed arguments:
- Mute: `users.prefs.set` with `name=muted_channels` and `value` the comma separated ids (§ notification settings above). `pref_change` brings it back.
- Fold a section: `users.channelSections.set` with `channel_section_id` and `is_collapsed=true|false`. GUESSED, method and keys; the registry has `.set`, `.create`, `.delete`, `.channels.bulkUpdate`, `.channels.remove` and `.entities.update` and no call site for any was recovered. A failed save leaves the fold in place here; it isn't kept past a restart, because boot's `is_collapsed` (also a guess) wins.
- Move or star: `users.channelSections.channels.bulkUpdate` with `insert` and `remove`, each a JSON list of `{channel_section_id, channel_ids}`. GUESSED. Starring is a move into the section whose `type` is `stars`; unstarring is a remove from it, which sends the conversation back to its kind's section.
- Events, bodies GUESSED from the handlers in §1 and each field optional: `channel_section_upserted` (`channel_section_id`, `name`, `emoji`, `channel_section_type`, `next_channel_section_id`, `is_collapsed`, `channel_ids_page.channel_ids`), `channel_section_deleted` (`channel_section_id`), `channel_sections_channels_upserted` and `_removed` (`channel_section_id`, `channel_ids`). `channel_joined`, `channel_left`, `im_created`, `im_close` and `mpim_close` are the classic RTM events.

## Other interesting methods (registry sample)

`chat.postMessage/update/delete/shareMessage`, `reactions.add/get/remove`, `pins.add/list/remove`, `bookmarks.*`, `emoji.*`, `users.list`,
`users.prefs.set`, `users.interactions.list/set`, `users.profile.get/set`, `team.info/counts/prefs.*`, `conversations.history`, `rooms.join/leave`,
`huddles.*`, `search.inline`, `files.external.preview`, `email.threads.share`, `ai.alpha.agents.threads.list`, `assistant.threads.rename`,
`meetings.processRecording`, `today.items.update`, `workflows.templates.get`, `functions.categories.steps.list`, `client.codeChannels.*`.

## Composer text on the wire (loafer's assumption)

`chat.postMessage` and `chat.update` take mrkdwn `text`: `<@U123>`, `<#C123>` (the web client adds `|name`, which is optional), `<!here>`, `<!channel>`, `<!everyone>`, links as `<url|label>`, and `&`, `<`, `>` as `&amp;`, `&lt;`, `&gt;`. loafer sends exactly that and has not been run against a live workspace yet. If Slack turns out to want `blocks` rich_text for mentions to ping, the composer's mention list already has what's needed to build them.

## Notification settings and typing (UNCERTAIN, from memory of the web client)

`internal/notify` reads these from `client.userBoot`'s `prefs` and `dnd`, and keeps them up with `pref_change` and `dnd_updated`. None is captured yet, so check each against a DevTools capture; a wrong key only means that setting is ignored.

- `prefs.muted_channels`, `prefs.highlight_words`, `prefs.at_channel_suppressed_channels`: comma separated strings. Fairly sure.
- `prefs.loud_channels`, `prefs.never_channels`: comma separated ids on everything and on nothing. A guess, and likely legacy.
- `prefs.all_notifications_prefs`: a string of JSON, `{global: {global_desktop, global_keywords, ...}, channels: {<id>: {desktop, muted, suppress_at_channel, ...}}}`. The level values (`everything`, `mentions`, `nothing`) and the `desktop` and `suppress_at_channel` names are guesses.
- `dnd`: `{dnd_enabled, next_dnd_start_ts, next_dnd_end_ts, snooze_enabled, snooze_endtime}`, in seconds. In dnd when now is from start to end, or before the snooze ends.
- `pref_change`: `{name, value}`, the value being what boot has for that name.
- `dnd_updated`: `{user, dnd_status: {...as dnd}}`. A guess. `dnd_updated_user` (other people's) is ignored.
- `user_typing`: `{channel, user}`, and `thread_ts` for typing in a thread. Sent for every conversation you're in, so the store keeps them and only wakes the UI for the open one. Presence is below.

## Reactions and emoji (loafer's assumptions)

`reactions.add` and `reactions.remove` take `channel`, `timestamp` and `name`, as the public API does; the registry above lists both but no call site was recovered. Errors loafer treats as "already how you wanted it": `already_reacted` and `no_reaction`. Names are iamcal/emoji-data's `short_name` (`+1`, not its alias `thumbsup`), with skin tones as `name::skin-tone-2` to `6`; loafer files a reaction under that canonical name so Slack's `reaction_added` event lands on the same chip. Two-tone names (`handshake::skin-tone-2-3`) are UNCERTAIN and not drawn as characters.

UNCERTAIN, not used yet: the web client seems to rank its picker by a `emoji_use` entry in `users.prefs` (a JSON map of name to count, by memory), which would give "frequently used" across runs. Loafer keeps this session's reactions instead.

## Pictures (UNCERTAIN, not tried against a live workspace)

`internal/images` fetches with `slack.Client.Fetch`, a plain GET.
- Private files (`files[].url_private`, `thumb_360`/`thumb_480`/`thumb_720` on `files.slack.com`) get the session's `d` cookie and nothing else, as a browser tab sends it. Whether the cookie alone is enough, or `files.slack.com` also wants `Authorization: Bearer xoxc-…` as it does for app tokens, is unchecked. If it 302s to a login page, the decode fails and the file shows as its line, as with graphics off.
- Avatars (`users.list` `profile.image_72`, a bot's `bot_profile.icons.image_72`) are on `avatars.slack-edge.com` or `secure.gravatar.com` and get no credentials. The cookie only ever goes over https to `slack.com` and its subdomains, and Go's client drops it on a redirect elsewhere.
- Image files are sized from `original_w`/`original_h`; image blocks from `image_width`/`image_height`, which are undocumented. Without them a picture takes no room until it lands.

## Files in and out (UNCERTAIN, not tried against a live workspace)

Sending is Slack's current three steps, from its public docs (the desktop app's own upload call wasn't recovered). `slack.Client.Upload` does them in `internal/slack/upload.go`:
1. `files.getUploadURLExternal` with `filename` and `length` (bytes) gives `upload_url` and `file_id`.
2. The bytes are POSTed to `upload_url`, raw, with a `Content-Type` of `application/octet-stream` and a `Content-Length`. The URL carries its own authority, so no cookie goes with it, and it isn't logged. The body streams from the file, and the request has no overall timeout, only the context.
3. `files.completeUploadExternal` with `files` (a JSON array of `{id, title}`, all the files at once), `channel_id`, and optionally `initial_comment` (the text), `thread_ts` and `reply_broadcast` (a thread reply that goes to the channel too, as `ctrl+b` ticks). It returns the files; the message arrives over the websocket as `file_share`, like anyone's.

Unchecked: that the web client's `xoxc-` session token and `d` cookie are taken by these methods as an app's token is, and `reply_broadcast`'s name. The limit is 1 GB a file, which loafer checks before sending. A failure at any step comes back as the usual `ok:false`, or an HTTP status from the upload host, and the files and words go back in the box.

Saving uses `Client.Download`, `Fetch`'s GET (cookie to Slack's hosts only) streamed to disk: `files[].url_private_download`, else `url_private`. As with pictures, whether the cookie alone is enough on `files.slack.com` is unchecked.

## Not recovered / suggested next step

views.* args, activity.markRead and subscriptions.thread.mark wire keys, users.channelSections.* args, client.dms args, search.modules messages/people,
flannel method enum, `conversations.history` extras. Best next source: DevTools on the running Electron app (network tab shows `/api/<method>` form bodies,
redact `token`), or the non-minified `gantry-v2` source maps if cached.

## People: presence, profiles, user groups (UNCERTAIN, from memory of the web client and the public API)

- `presence_sub`: `{"type":"presence_sub","ids":[...]}` sent over the websocket, 100 ms after the last id added (this one is recovered from the bundle, above). It belongs to the socket, so loafer sends everyone again after a reconnect. It never unsubscribes. Whether a large id list wants chunking is unknown.
- `presence_change`: `{user, presence}` or, with `batch_presence_aware=1`, `{users: [...], presence}`. `presence` is `active` or `away`. The batch key name `users` is a guess. `manual_presence_change`: `{presence}`, which is you.
- `user_change` carries the whole user. `user_status_changed` is assumed to carry `user` with at least `id` and `profile.status_*`; loafer takes a user without a name as a status change only. Status expiry is `profile.status_expiration`, unix seconds, 0 for none.
- `users.info` with `user`, answering `{user}`; `users.list` fields `profile.title`, `email`, `pronouns` (pronouns may be absent or sit in a custom field), `tz`. Email is present only where the workspace shows it.
- `usergroups.list` with `include_count=true`, answering `{usergroups: [{id, handle, name, user_count, date_delete}]}`. A failure is only logged. `subteam_created` and `subteam_updated` are assumed to carry the group whole as `subteam`; `date_delete` non-zero means gone. `subteam_members_changed` and `subteam_self_*` are ignored.
- `conversations.open` with `users=<id>` and `return_im=true`, answering `{channel}`; `already_open` is a normal answer carrying the channel.
- Sent as `<!subteam^S123|@handle>`, read as `<!subteam^S123>` with or without a label, and as a rich_text `usergroup` element.
