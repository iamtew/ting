# tng

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

## v1: one process

```text
IRC -- gateway -- chan Event -- admin / links / logger / ai
```

In-process `chan` is the bus. Split into processes (NATS, whatever) when a module crash taking down IRC is a real problem.

Gateway: connect, TLS, SASL PLAIN, NickServ, join/part, reconnect, outbound queue, channel/user maps.

Not the gateway: commands, moderation, URLs, AI.

## t3b parity (before new toys)

Connectivity: TLS, SASL PLAIN, nick/user/realname, autojoin (many channels), reconnect, Win/Linux.

Auth: owner / admin / user via `nick!user@host` with wildcards.

Admin: `.join` `.leave` `.op` `.deop`

Owner/admin: `.help` `.status` `.say`

Owner: `.stop` `.restart` `.reload` `.nick`

Automode: if we have +o, keep owners/admins +o.

## Later (not now)

- URL titles (generic, YouTube, X)
- SQLite message/event log
- AI: mention replies, `.summary`, optional memory
- Dashboard / extra networks / Discord etc. when v1 is boring

## Build

1. Gateway + in-process bus + join/recv/send/reconnect.
2. Permissions, admin commands, automode.
3. Then links, then sqlite, then AI.
