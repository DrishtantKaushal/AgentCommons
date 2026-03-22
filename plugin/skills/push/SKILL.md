---
name: push
description: Send a message or task to another terminal. Use when the user says "send X to #Name", "tell #Name to do X", or "push this to the other terminal".
argument-hint: "<agent-name> <message>"
allowed-tools: mcp__commons__commons_push
---
# Commons Push
Parse `$ARGUMENTS`: first word is the target agent name (strip # or @), rest is the message. Call `commons_push` MCP tool. If the user's intent is to delegate work (tell/ask/run/delegate), set type to "task". If informational (send/share/FYI), set type to "message".
