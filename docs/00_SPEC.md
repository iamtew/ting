MISSION

Build a resilient IRC platform using Go.

The system must separate IRC connectivity from bot logic.

The IRC Gateway is the sole component that maintains a
connection to IRC servers.

All IRC activity is translated into structured events and
published onto an event bus.

Consumers such as bots, dashboards, analytics, logging,
and AI modules subscribe to events and may be restarted
independently without impacting the IRC connection.

DESIGN PRINCIPLES

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

---

# IRC Platform v1

Predecessor projects we shall take advantage of:
- https://github.com/iamtew/t3b
- https://github.com/iamtew/subotto

## Vision

Build a resilient, long-running IRC platform in Go.

The platform separates IRC connectivity from bot behavior.

A dedicated IRC Gateway maintains server connections, channel membership, presence, and state.

All IRC activity is transformed into events.

Modules consume events and provide functionality such as:

- Moderation
- URL resolution
- Administration
- AI assistants
- Summaries
- Analytics
- Automation

Modules may be restarted independently without affecting IRC connectivity.

---

# Core Architecture

```text
                IRC SERVER
                     |
                     |
              +------v------+
              | IRC Gateway |
              +------+------+ 
                     |
                     |
                Event Bus
                     |
    +--------+-------+-------+--------+
    |        |               |        |
+---v---+ +--v----+    +-----v--+ +---v----+
| Admin | | Links |    | AI Core| | Logger |
+-------+ +-------+    +--------+ +--------+
```

---

# Design Principles

1. Gateway owns IRC connections.
2. Business logic never touches IRC sockets.
3. Everything is an event.
4. Components communicate through IPC.
5. Components are independently deployable.
6. SQLite is acceptable as the primary datastore.
7. Prefer Go standard library when practical.
8. Design for multi-network support from day one.
9. Graceful reconnection is mandatory.
10. Gateway must operate unattended for months.

---

# Services

## IRC Gateway

Responsibilities:

- IRC connection lifecycle
- Reconnection handling
- SASL authentication
- NickServ integration
- Join / Part operations
- Tracking channel state
- Tracking user state
- Outbound message queue

Non-responsibilities:

- AI features
- Commands
- Moderation
- URL parsing
- Business logic

---

## Event Bus

Preferred technology:

```text
NATS
```

Subjects:

```text
irc.message
irc.join
irc.part
irc.quit
irc.nick
irc.notice
irc.command
```

---

## State Service

Maintains:

- Server state
- Channel state
- User state

Provides:

- REST API
- WebSocket API

---

# Compatibility Requirements (t3b)

The successor platform must achieve feature parity with t3b before major new functionality is introduced.

## Connectivity

- TLS
- SASL PLAIN
- Configurable nick/user/realname
- Autojoin channels
- Multiple channels
- Graceful reconnection
- Windows support
- Linux support

## Permissions

Roles:

- Owner
- Admin
- User

Authorization:

```text
nick!user@host
```

Wildcard hostmask matching required.

## Administrative Commands

Admin:

```text
.join
.leave
.op
.deop
```

Owner/Admin:

```text
.help
.status
.say
```

Owner only:

```text
.stop
.restart
.reload
.nick
```

## Automode

When the bot has operator privileges it should ensure owner and admin accounts retain operator status.

---

# Link Intelligence Module

Consumes:

```text
irc.message
```

Produces:

```text
resolver.result
```

Capabilities:

## Generic URLs

- Title
- Content type
- Optional size information

## YouTube

- Title
- Channel
- Duration
- Publish date
- View count

## Social Media

Initial:

- X / Twitter

Future:

- Reddit
- Instagram

---

# AI Platform

Subotto-inspired capabilities should be implemented as event consumers.

## AI Context Engine

Maintains:

- Rolling channel context
- Recent conversation history
- Active participants
- Mentioned topics

## AI Mention Assistant

Triggers:

```text
botnick:
@bot
```

Pipeline:

```text
IRC
 -> Event
 -> AI Service
 -> LLM
 -> Response Event
 -> IRC
```

## AI Summaries

Commands:

```text
.summary 1h
.summary today
.summary 500 lines
```

Output:

- Key topics
- Important decisions
- Shared links
- Participant highlights
- Action items

## AI Memory

Modes:

```text
memory = off
memory = window
memory = persistent
```

Storage:

- SQLite initially
- PostgreSQL later if needed

## AI Link Enrichment

Optional AI-generated:

- TLDRs
- Key points
- Sentiment
- Tags

## AI Operator

Future capability.

The AI may observe operational events and suggest:

- Moderation actions
- Recovery actions
- Administrative actions

Human approval required initially.

---

# Storage

SQLite first.

Suggested tables:

```text
messages
events
users
channels
permissions
summaries
ai_memory
resolver_cache
```

---

# Non-Functional Requirements

- Gateway uptime measured in months.
- Modules must never crash the gateway.
- Structured logging.
- Service boundaries documented.
- Full observability support.
- TOML-based configuration.
- Single-binary deployment where practical.
- Windows and Linux support.

---

# Implementation Roadmap

## Phase 1

- IRC Gateway
- NATS
- Join channels
- Receive messages
- Send messages
- Reconnect logic

## Phase 2

- Permissions
- Administrative commands
- Automode

## Phase 3

- URL resolver
- YouTube resolver
- Social media resolver

## Phase 4

- SQLite event store
- Message history

## Phase 5

- AI context engine
- AI replies
- AI summaries
- AI memory

## Phase 6

- Dashboard
- Metrics
- Observability

## Phase 7

- Multi-network support

## Phase 8

- Discord adapter
- Twitch adapter
- Matrix adapter

---

# Architectural Rule

Any feature currently implemented in t3b or Subotto must be implementable as an event consumer.

No feature may require direct access to an IRC socket.
