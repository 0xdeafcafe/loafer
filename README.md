<h1 align="center">loafer</h1>

<p align="center">
  slack in your terminal, written in go, in rush's clothes.<br>
  it's built to be quick and light, and it's early: it opens, reads, sends and edits, and not much else yet.
</p>

## install

```sh
go install github.com/0xdeafcafe/loafer/cmd/loafer@latest
```

needs go 1.27.1 or newer, and macos for now (sign-ins live in the keychain).

```sh
loafer login      # sign in with your slack session
loafer            # open it
loafer app init   # loafer's own slack app, for phone pushes and shortcuts
loafer report     # zip the logs and latest profile for a bug report
```

## signing in

loafer talks to slack the way the desktop app does, with your session's `xoxc-` token and its `d` cookie, so it sees everything you do and needs no admin to approve anything. `loafer login` tells you where to copy both from (the desktop app's devtools, or the browser's) and checks them with slack before keeping them in the keychain.

it doesn't go looking for them itself. reading another app's cookies is the sort of thing you should say yes to first, so that waits until you do.

## what it does

- **the sidebar**: your sections in slack's order, unread in bold, mentions in yellow.
- **conversations**: slack's mrkdwn and markdown both, so bots' headings, lists and tables come out as they meant them, and ```go blocks are highlighted (rush's highlighter, 15 or so languages). then attachments, reactions, threads' reply counts and a line where you'd read up to. drawn from the cache first, so it opens before slack has answered.
- **keys for everything**: `ctrl+k` jumps anywhere, `ctrl+n` goes to whatever needs you, `alt+←` goes back, and there's a cursor over the messages for editing, binning, copying and opening links. the full list is in [docs/ui.md](docs/ui.md#keys).
- **drafts** stay with their conversation, and `↑` in an empty box edits your last message, as slack does.
- **mentions**: type `@` or `#` and a list of people or channels opens above the box. the box shows `@Alex`, slack is sent `<@U123>`, and editing a message turns them back.

on the way, in this order: live updates over the websocket, threads, block kit buttons and modals, images, dms, activity, later, search and notifications. then a claude pane, which needs [rush](https://github.com/0xdeafcafe/rush) since it's your ai doing the work.

## when it goes wrong

logs are in `~/Library/Logs/loafer`, with tokens and cookies redacted on the way in. `--trace api` logs every request and response body too, `f12` shows memory, cpu and frame times along the bottom, and `ctrl+alt+p` profiles for 35 seconds. `loafer report` zips the lot.

## how it works

slack's own web client calls the same api as everyone else, plus a few internal methods (`client.userBoot`, `client.counts`, `users.channelSections.list`) for the sidebar. [docs/slack-internal-api.md](docs/slack-internal-api.md) has what loafer uses and how it found out. none of that is documented, so a slack update can break it.

the screen is rows of styled text kept per message, so a frame only redraws what's on it. [docs/design.md](docs/design.md) has the rest.
