---
name: deny
description: Deny a pending approval request for a named agent terminal. Use when the user says "deny #Name", "/commons:deny Name", or wants to reject a blocked terminal's request.
argument-hint: "<agent-name>"
allowed-tools: mcp__commons__commons_deny
---
# Commons Deny
Take the agent name from `$ARGUMENTS`, strip any `#` or `@` prefix. Call the `commons_deny` MCP tool with that target name. Report which agent was denied. If no name provided, first call `commons_status` to show which agents are blocked.
