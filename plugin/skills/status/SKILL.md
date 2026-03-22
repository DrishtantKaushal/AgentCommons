---
name: status
description: Show all agent terminals and their current state in AgentCommons. Use when the user asks about other terminals, what agents are running, mentions #AgentName, or wants to check terminal status.
allowed-tools: mcp__commons__commons_status
---
# Commons Status
Call the `commons_status` MCP tool (no arguments needed) and present the results. Highlight any terminals blocked on approval and suggest `/commons:approve <name>` to unblock.
