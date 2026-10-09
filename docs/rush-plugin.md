# the rush plugin

status: built, 2026-10-09, tested against `internal/slacktest` with a fake rush. not yet run under rush's real sandbox and proxy against slack.

`cmd/loafer-rush` is loafer as a [rush](https://github.com/0xdeafcafe/rush) plugin: the agents you run in rush can search your slack, read a conversation or thread, see what's unread, look someone up, and leave a draft in loafer's composer. it reads slack with loafer's own sign-in. nothing it does sends, reacts or marks anything read.

it's its own binary, not `loafer rush-plugin`. rush records every file in a plugin's folder when you approve it and stops the plugin when one changes, so a separate binary means rebuilding loafer doesn't stop your agents' slack, and the program in the sandbox is only the plugin.

## install

```sh
go install github.com/0xdeafcafe/loafer/cmd/loafer@latest   # loafer draft needs it
mkdir -p ~/.config/rush/plugins/loafer
go build -o ~/.config/rush/plugins/loafer/loafer-rush ./cmd/loafer-rush
cp cmd/loafer-rush/plugin.json ~/.config/rush/plugins/loafer/
```

on apple silicon, build with `GOARCH=arm64` if `go env GOARCH` says `amd64`: the sandbox blocks rosetta.

then change two lines of `~/.config/rush/plugins/loafer/plugin.json`:

- `network`: your workspace's host, as loafer signed in to it, e.g. `"acme.slack.com:443"`. it's the only host the plugin can reach.
- `exec.loafer`: where `loafer` is, if not `~/go/bin/loafer`.

it uses loafer's first workspace, the one `loafer` opens. you need to have run `loafer login`.

## approve

```sh
rush plugin check loafer
rush plugin approve loafer
```

approving shows what it may do. for this plugin that's:

- **reach** your workspace's host on 443, through rush's proxy, and nothing else.
- **read** `~/Library/Application Support/loafer/workspaces.json`, the list of signed-in workspaces (no secrets in it), to know which team is yours.
- **run, outside the sandbox, as you**:
  - `keychain`: `/usr/bin/security find-generic-password -s loafer -w`, with `-a <team id>` added, to read loafer's sign-in. the sandbox can't reach the keychain or start programs, so rush runs it. the plugin only ever asks for loafer's item, by a team id it checks first; the approval is for the binary, so that's what holds it to that.
  - `loafer`: `loafer draft`, with the team, conversation and thread added and the text on stdin. it hands the draft to the running loafer over a unix socket only you can reach.

every rebuild of `loafer-rush` needs approving again. each tool call asks you before it runs, unless you allow it.

## the tools

claude sees them as `mcp__rush-loafer__<tool>`.

| tool | what it does |
|---|---|
| `slack_search` | `search.messages`, 20 a page, slack's modifiers and all. each hit has where it was and its link |
| `slack_read` | a conversation's latest messages (50, up to 200), or a thread whole. takes `#channel`, `@person` (your dm with them), an id, or a message's link, which reads its thread |
| `slack_unread` | each conversation with unread messages and how many mention you, mentions first, then dms. refreshed from `client.counts` on each call |
| `slack_who` | a person: name, handle, title, timezone and their time now, status, and your dm with them |
| `slack_draft` | puts text in loafer's composer for a conversation or thread, after anything already there |

text comes back as the claude pane writes it ([claude-pane.md](claude-pane.md)): one line a message, oldest first, names resolved, files as `[file: name]`, at most 32 KB an answer, the oldest dropped first.

`slack_draft` goes into the box on screen if it's that conversation, else into the conversation's kept draft, which is there when you open it. if you're editing a message there, it's refused rather than mixed into the edit. if loafer isn't running, nothing is saved: slack has a drafts api (`drafts.create`), but its arguments aren't confirmed, so the tool says so and hands the agent the text to give you.

## safety

- **no tool sends.** a message goes out under your name, so sending it is yours: the draft lands in loafer's box and waits for you. the tools' descriptions and the prompt rush adds to every session say so, so an agent doesn't go looking for a way round it.
- **slack is untrusted input.** anyone in your workspace can write what an agent reads. every answer goes inside a `<slack>` element, under the pane's line saying it's material to read, not instructions. a `</slack>` in a message (or a name) is broken up so the frame stays loafer's.
- **no secrets leave.** the sign-in goes from the keychain to the plugin through rush and is never logged; the plugin's log has method names and timings, as loafer's does.
- **nothing changes in slack.** no reading marks anything read, and nothing reacts, posts or edits.

## not done

- **counts in rush's sidebar.** rush's `sidebar.set` arranges its agent list under the plugin's sections; it has nowhere to put a count of its own, so a section titled with your unreads would be an empty group. `slack_unread` is there instead. if rush grows a status line for plugins, polling `client.counts` once a minute is cheaper than holding the websocket in the plugin.
- other workspaces than the first, and channels you're not in by name (an id or link works).
