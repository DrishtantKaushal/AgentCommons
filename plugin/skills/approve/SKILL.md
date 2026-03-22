---
name: approve
description: Approve a pending approval request for a named agent terminal. Use when the user says "approve #Name", "/commons:approve Name", or wants to approve a blocked terminal.
argument-hint: "<agent-name>"
allowed-tools: mcp__commons__commons_approve
---
# Commons Approve
Take the agent name from `$ARGUMENTS`, strip any `#` or `@` prefix. Call the `commons_approve` MCP tool with that target name. Report which agent was approved. If no name provided, first call `commons_status` to show which agents are blocked.
