---
name: inbox
description: Check pending notifications from other agent terminals. Use when the user asks about inbox, notifications, approvals needed, "any updates?", or "what did I miss?".
allowed-tools: mcp__commons__commons_inbox
---
# Commons Inbox
Call the `commons_inbox` MCP tool and present any pending notifications. For approval requests, suggest `/commons:approve <name>` or `/commons:deny <name>`. Note that with channel push active, most messages arrive live — inbox only holds messages that couldn't be delivered in real time. For full message history, use `commons_history`.
