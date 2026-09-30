# TiNG

Go IRC platform. Gateway owns the sockets. Everything else consumes events.

Predecessors: [t3b](https://github.com/iamtew/t3b), [subotto](https://github.com/iamtew/subotto).

## MISSION

Build a resilient IRC platform using Go.

The system must separate IRC connectivity from bot logic.

The IRC Gateway is the sole component that maintains a
connection to IRC servers.

All IRC activity is translated into structured events and
published onto an event bus.

Consumers such as bots, dashboards, analytics, logging,
and AI modules subscribe to events and may be restarted
independently without impacting the IRC connection.

## DESIGN PRINCIPLES

1. Gateway owns the IRC connection.
2. Business logic never touches IRC sockets.
3. Everything is an event.
4. Components communicate over IPC.
5. Components must be independently deployable.
6. SQLite is acceptable for local persistence.
7. Prefer standard library where practical.
8. Design for multi-server support from day one.
9. Graceful reconnection is mandatory.
10. The gateway must operate unattended for months.

## Rules

1. Only the gateway talks IRC.
2. Modules never touch the socket. If t3b or subotto had it, it is a consumer.
3. stdlib first. SQLite if we persist. TOML config.
4. Reconnect. Unattended for months. Windows + Linux.

## v1: two processes (split early)

```text
IRC -- connector (gateway) -- HTTP localhost control -- master (admin UI + commands + SQLite)
```

Gateway owns the socket (one process per IRC server). Master owns the admin UI, SQLite (`ting.db`: servers, channels, owners/admins), and spawns connectors. IPC is stdlib HTTP on loopback (not NATS). Global nick/user/realname stay in TOML `[identity]`; per-server override in the DB.

Gateway: connect, TLS, SASL PLAIN, NickServ, join/part, reconnect, outbound queue, channel/user maps.

Not the gateway: commands, moderation, URLs, AI.

## t3b parity (before new toys)

Connectivity: TLS, SASL PLAIN, nick/user/realname, autojoin (many channels), reconnect, Win/Linux.

Auth: owner / admin / user via `nick!user@host` with wildcards.

URL resolve (master, not the connector): first http(s) URL in a channel PRIVMSG. Modules, first match wins: Twitter/X, Bluesky, YouTube (Data API v3 when a key is set), Reddit, generic page title. Per-server enable/disable + YouTube key in the admin server tab (same knobs as t3b `[resolve]`). 6 titles per channel per minute. Resolved links persist in SQLite. Admin **links** tab: search and browse. **import** tab: drag-drop t3b `links-*.log` JSONL or `karma-*.db` onto a server (link duplicates skipped; karma scores upserted).

Karma (master): channel `phrase++` / `phrase--` (silent ±1), `+d`/`-d` (dice 1–6), `+N..M`/`-N..M` (random, max 23). Public `.karma` / `.karma <phrase>` in channel or DM. Scores per server in `ting.db`.

Admin: `.join` `.leave` `.op` `.deop`

Owner/admin: `.help` `.status` `.say`

Owner: `.stop` `.restart` `.reload` `.nick`

Automode: if we have +o, keep owners/admins +o.

## Later (not now)

- SQLite message/event log
- AI: mention replies, `.summary`, optional memory
- Discord etc. when v1 is boring
- `.restart` / `.reload` / automode

## Build

1. Gateway + join/recv/send/reconnect.
2. Control HTTP + master admin UI + permissions + admin commands.
3. SQLite servers + master-spawned connectors.
4. Channel URL resolve (master). Then message log, then AI.
