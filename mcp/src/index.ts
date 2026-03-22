import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { DaemonClient } from "./daemon-client.js";
import { handleStatus, handleInbox, handleHistory, handleReportState, NotificationQueue } from "./tools.js";
import { formatBootstrap } from "./bootstrap.js";
import fs from "fs";
import path from "path";
import os from "os";

// Use low-level Server (not McpServer) to declare claude/channel capability
const server = new Server(
  { name: "commons", version: "0.1.0" },
  {
    capabilities: {
      tools: { listChanged: true },
      experimental: { "claude/channel": {} },
    },
    instructions:
      "IMPORTANT: This MCP server connects you to AgentCommons — other Claude Code terminals running on this machine. " +
      "When the user mentions #Name (like #Zander, #FreshRidge, #Maven), they are referring to another terminal agent. " +
      "NEVER execute commands locally when the user addresses #Name — ALWAYS route through commons_push instead. " +
      "Examples: '#FreshRidge list the folders' → commons_push(target='FreshRidge', message='list the folders'). " +
      "'what is #Maven doing' → commons_status. 'send hi to #Zander' → commons_push(target='Zander', message='hi'). " +
      "Use commons_status to see all terminals. Use commons_inbox to check notifications.",
  }
);

const client = new DaemonClient();
const notifications = new NotificationQueue();

// Track the latest prompt_hash per slot so commons_approve/commons_deny can
// include it in the approval_response. Without this, the target wrapper's
// injector rejects the response due to empty/mismatched hash.
const lastApprovalHashes: Map<string, string> = new Map();

let sessionId = "";
let slotId = "";
let slotName = "";
let notificationMode: "channel" | "inbox" = "channel"; // default: live push
const isWrapperMode = !!process.env.COMMONS_SESSION_ID;
let wrapperBlocked = false;

// --- Tool Registration ---

server.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [
    {
      name: "commons_status",
      description:
        "Show all agent terminals registered with AgentCommons on this machine. " +
        "USE THIS TOOL WHEN the user asks about other terminals, mentions #AgentName, " +
        'or asks "what is the other terminal doing?", "who is running?", "show agents".',
      inputSchema: { type: "object" as const, properties: {} },
    },
    {
      name: "commons_inbox",
      description:
        "Check pending notifications from other agent terminals. " +
        "USE THIS TOOL WHEN the user asks about inbox, notifications, approvals needed, " +
        'or "any updates?", "what did I miss?".',
      inputSchema: { type: "object" as const, properties: {} },
    },
    {
      name: "commons_push",
      description:
        "Send a message or task to another agent terminal via AgentCommons. " +
        "USE THIS TOOL WHEN the user wants to communicate with or delegate work to another terminal. " +
        "Always use this when the user mentions #Name or @Name alongside a message, instruction, or question. " +
        "The receiving terminal will act on whatever you send. " +
        'E.g. "#FreshRidge list the folders", "ask #Maven how his day was", "tell #Zander auth is broken".',
      inputSchema: {
        type: "object" as const,
        properties: {
          target: { type: "string", description: "Target agent name (e.g. Zander, Maven)" },
          message: { type: "string", description: "Message content to send" },
        },
        required: ["target", "message"],
      },
    },
    {
      name: "commons_history",
      description:
        "Show message history between terminals. Use when user asks about past messages, " +
        "conversation history, or what was sent/received. Shows both delivered and pending messages.",
      inputSchema: {
        type: "object" as const,
        properties: {
          limit: { type: "number", description: "Number of messages to show (default 20)" },
        },
      },
    },
    {
      name: "commons_approve",
      description:
        "Approve a pending approval request for another terminal. " +
        "Use when user says 'approve #Name', '/commons:approve Name', or when you see a terminal is blocked on approval.",
      inputSchema: {
        type: "object" as const,
        properties: {
          target: { type: "string", description: "Agent name to approve (e.g. Zander, Maven)" },
        },
        required: ["target"],
      },
    },
    {
      name: "commons_deny",
      description:
        "Deny a pending approval request for another terminal. " +
        "Use when user says 'deny #Name' or '/commons:deny Name'.",
      inputSchema: {
        type: "object" as const,
        properties: {
          target: { type: "string", description: "Agent name to deny (e.g. Zander, Maven)" },
        },
        required: ["target"],
      },
    },
    {
      name: "commons_report_state",
      description: "Report this agent's state change to AgentCommons immediately.",
      inputSchema: {
        type: "object" as const,
        properties: {
          state: { type: "string", description: "New state: working, idle, blocked_on_approval, error" },
          state_detail: { type: "string", description: "Human-readable context" },
        },
        required: ["state"],
      },
    },
  ],
}));

server.setRequestHandler(CallToolRequestSchema, async (request) => {
  const { name, arguments: args } = request.params;

  switch (name) {
    case "commons_status": {
      const text = await handleStatus(client);
      return { content: [{ type: "text" as const, text }] };
    }
    case "commons_inbox": {
      const text = handleInbox(notifications);
      return { content: [{ type: "text" as const, text }] };
    }
    case "commons_push": {
      const target = ((args as any)?.target || "").replace(/^[#@]/, "");
      const message = (args as any)?.message || "";
      if (!target || !message) {
        return { content: [{ type: "text" as const, text: "Both target and message are required" }], isError: true };
      }
      if (!client.isConnected) {
        return { content: [{ type: "text" as const, text: "Not connected to daemon" }], isError: true };
      }
      try {
        const resp = await client.send("push_message", { target_slot_name: target, content: message, message_type: "task" });
        if (resp.payload.error) {
          return { content: [{ type: "text" as const, text: `Failed: ${resp.payload.message}` }], isError: true };
        }
        return { content: [{ type: "text" as const, text: `Sent to #${target}. They'll handle it — use /commons:status to check progress.` }] };
      } catch (err) {
        return { content: [{ type: "text" as const, text: `Error: ${err}` }], isError: true };
      }
    }
    case "commons_history": {
      const limit = (args as any)?.limit || 20;
      const text = await handleHistory(client, limit);
      return { content: [{ type: "text" as const, text }] };
    }
    case "commons_approve": {
      const target = ((args as any)?.target || "").replace(/^[#@]/, "");
      if (!target) {
        return { content: [{ type: "text" as const, text: "Target agent name is required" }], isError: true };
      }
      if (!client.isConnected) {
        return { content: [{ type: "text" as const, text: "Not connected to daemon" }], isError: true };
      }
      try {
        // Include the prompt_hash from the last approval broadcast for this target.
        // Without this, the target wrapper's injector rejects the approval due to
        // hash mismatch (it compares pending.Hash against the incoming prompt_hash).
        const promptHash = lastApprovalHashes.get(target) || "";
        const resp = await client.send("approval_response", {
          action: "approve",
          target_slot_name: target,
          prompt_hash: promptHash,
        });
        if (resp.payload.error) {
          return { content: [{ type: "text" as const, text: `Failed: ${resp.payload.message}` }], isError: true };
        }
        lastApprovalHashes.delete(target);
        return { content: [{ type: "text" as const, text: `Approved #${target} — they should resume shortly.` }] };
      } catch (err) {
        return { content: [{ type: "text" as const, text: `Error: ${err}` }], isError: true };
      }
    }
    case "commons_deny": {
      const target = ((args as any)?.target || "").replace(/^[#@]/, "");
      if (!target) {
        return { content: [{ type: "text" as const, text: "Target agent name is required" }], isError: true };
      }
      if (!client.isConnected) {
        return { content: [{ type: "text" as const, text: "Not connected to daemon" }], isError: true };
      }
      try {
        const promptHash = lastApprovalHashes.get(target) || "";
        const resp = await client.send("approval_response", {
          action: "deny",
          target_slot_name: target,
          prompt_hash: promptHash,
        });
        if (resp.payload.error) {
          return { content: [{ type: "text" as const, text: `Failed: ${resp.payload.message}` }], isError: true };
        }
        lastApprovalHashes.delete(target);
        return { content: [{ type: "text" as const, text: `Denied #${target}'s approval request.` }] };
      } catch (err) {
        return { content: [{ type: "text" as const, text: `Error: ${err}` }], isError: true };
      }
    }
    case "commons_report_state": {
      const text = await handleReportState(
        client,
        sessionId,
        (args as any)?.state || "idle",
        (args as any)?.state_detail
      );
      return { content: [{ type: "text" as const, text }] };
    }
    default:
      return { content: [{ type: "text" as const, text: `Unknown tool: ${name}` }], isError: true };
  }
});

// --- Channel Push ---

async function pushViaChannel(content: string, meta: Record<string, string> = {}) {
  // Suppress channel pushes while the wrapper is handling an approval prompt
  // to prevent TUI corruption from concurrent Ink redraws
  if (wrapperBlocked) {
    notifications.push(content);
    return;
  }

  if (notificationMode !== "channel") {
    // Inbox mode — queue instead of pushing
    notifications.push(content);
    return;
  }

  try {
    await server.notification({
      method: "notifications/claude/channel",
      params: { content, meta },
    });
  } catch {
    // Channel not available — fall back to inbox queue
    notifications.push(content);
  }
}

// --- Lifecycle ---

async function main() {
  // CRITICAL: Start MCP server FIRST so Claude Code gets the initialize response immediately
  const transport = new StdioServerTransport();
  await server.connect(transport);

  // Connect to daemon asynchronously (non-blocking)
  connectToDaemon().catch((err) => {
    console.error("[commons] daemon connection failed:", err);
  });
}

async function connectToDaemon() {
  try {
    await client.connect();
  } catch (err) {
    console.error("[commons] Failed to connect to daemon:", err);
    return;
  }

  if (!client.isConnected) return;

  // Check if running under the wrapper (wrapper already registered this session).
  // If COMMONS_SESSION_ID is set, subscribe to that session instead of registering.
  const wrapperSessionId = process.env.COMMONS_SESSION_ID || readWrapperSessionFile();

  let subscribed = false;

  if (wrapperSessionId) {
    // Subscribe mode: the wrapper already claimed the slot, so the MCP server
    // just subscribes to receive events routed to that session.
    try {
      const resp = await client.send("subscribe", {
        session_id: wrapperSessionId,
      });

      const payload = resp.payload;
      if (payload.error) {
        console.error(`[commons] Subscribe failed: ${payload.message}`);
        return;
      }

      // If the daemon confirms a wrapper is active, trust that over the env/file detection
      if (payload.wrapper_active === false && !process.env.COMMONS_SESSION_ID) {
        // Stale file entry — wrapper is not actually running, fall through to register
        console.error("[commons] Subscribe succeeded but no wrapper active, falling through to register");
        // Don't set subscribed — fall through to standalone register below
      } else {
        sessionId = wrapperSessionId;
        slotName = process.env.COMMONS_SLOT_NAME || deriveTerminalName();
        console.error(`[commons] Subscribed to session ${sessionId} as ${slotName}`);
        subscribed = true;
      }
    } catch (err) {
      console.error("[commons] Subscribe error:", err);
      return;
    }
  }

  if (!subscribed) {
    // Standalone mode: no wrapper, so register directly with the daemon.
    try {
      const terminalName = process.env.COMMONS_SLOT_NAME || deriveTerminalName();
      const resp = await client.send("register", {
        agent_type: "claude-code",
        terminal_name: terminalName,
        pid: process.pid,
        cwd: process.cwd(),
        repo_root: "",
        claude_session_id: process.env.CLAUDE_SESSION_ID || "",
      });

      const payload = resp.payload;
      if (payload.error) {
        console.error(`[commons] Registration failed: ${payload.message}`);
        return;
      }

      sessionId = payload.session_id;
      slotId = payload.slot_id;
      slotName = terminalName;

      // Bootstrap on slot reclaim
      if (!payload.is_new_slot && payload.slot_context) {
        const bootstrapMsg = formatBootstrap(
          terminalName,
          payload.slot_context.previous_sessions + 1,
          payload.slot_context
        );
        // Push bootstrap via channel (live) so agent sees reconnection context
        pushViaChannel(bootstrapMsg, { type: "bootstrap", slot: terminalName });
      }
    } catch (err) {
      console.error("[commons] Registration error:", err);
      return;
    }
  }

  // Listen for approval broadcasts — push live by default.
  // In subscribe mode (wrapper is active), the wrapper's NotificationManager
  // already handles rendering and keystroke interception for approval
  // broadcasts. Pushing a duplicate via the channel causes double-rendering.
  // Only queue to notifications for /inbox lookup (no live push).
  client.on("approval_broadcast", (msg) => {
    const p = msg.payload;
    const ago = Math.round((Date.now() - new Date(p.requested_at).getTime()) / 1000);
    const content = [
      `#${p.slot_name} needs approval (${ago}s ago):`,
      `  ${p.prompt_text}`,
      `  To approve: tell me to approve #${p.slot_name}`,
    ].join("\n");

    // Always store the hash (needed for commons_approve/commons_deny in both modes)
    lastApprovalHashes.set(p.slot_name, p.prompt_hash);

    if (isWrapperMode || wrapperSessionId) {
      // Wrapper mode — only queue for /inbox, wrapper handles live UI
      notifications.push(content);
    } else {
      // Standalone mode — push via channel
      pushViaChannel(content, {
        type: "approval_request",
        from: p.slot_name,
        prompt_hash: p.prompt_hash,
      });
    }

    // If THIS terminal sent the approval request (we are the blocked one),
    // suppress channel pushes to prevent TUI corruption during injection
    if (wrapperSessionId && p.session_id === sessionId) {
      wrapperBlocked = true;
      setTimeout(() => { wrapperBlocked = false; }, 5000);
    }
  });

  // Listen for state changes
  client.on("agent_state_changed", (msg) => {
    const p = msg.payload;
    if (p.new_state === "disconnected") {
      pushViaChannel(`#${p.slot_name} disconnected`, { type: "state_change", from: p.slot_name });
    }
  });

  // Listen for pushed messages from other terminals.
  // All pushes are treated uniformly — the receiving Claude decides how to act.
  client.on("message_push", (msg) => {
    const p = msg.payload;
    pushViaChannel(
      `From #${p.from_slot_name}: ${p.content}`,
      { type: "task", from: p.from_slot_name, sent_at: p.sent_at }
    );
  });

  // Approval result handlers — clear the channel suppression flag.
  // In wrapper mode, sendToWrapperOnly() routes these only to the Go wrapper,
  // so these handlers are effectively dead code. The wrapperBlocked flag is
  // primarily cleared by the 5-second setTimeout in the approval_broadcast
  // handler. These handlers serve as belt-and-suspenders for standalone mode
  // and as documentation of intent.
  client.on("approval_granted", () => {
    wrapperBlocked = false;
  });

  client.on("approval_denied", () => {
    wrapperBlocked = false;
  });

  // Heartbeat loop
  const heartbeatInterval = setInterval(() => {
    if (!client.isConnected || !sessionId) return;
    client.sendFire("heartbeat", {
      session_id: sessionId,
      state: "idle",
      state_detail: "",
      cwd: process.cwd(),
    });
  }, 10000);

  // Cleanup on exit
  const cleanup = () => {
    clearInterval(heartbeatInterval);
    // Only send deregister in standalone mode (no wrapper).
    // In subscribe mode the wrapper owns the session lifecycle and sends
    // its own deregister — sending a second one here causes a race that
    // can broadcast spurious agent_state_changed events to other terminals
    // and corrupt their connection state (see bug-009).
    if (client.isConnected && sessionId && !wrapperSessionId) {
      client.sendFire("deregister", { session_id: sessionId });
    }
    setTimeout(() => {
      client.close();
      process.exit(0);
    }, 200);
  };

  process.on("SIGTERM", cleanup);
  process.on("SIGINT", cleanup);
}

function deriveTerminalName(): string {
  const cwd = process.cwd();
  const basename = cwd.split("/").pop() || "Agent";
  return basename
    .split(/[-_]/)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join("");
}

function getTTYKey(): string {
  // Mirror Go's TerminalKey() from naming/slots.go
  try {
    return fs.readlinkSync("/dev/fd/0");
  } catch {}
  if (process.env.TERM_SESSION_ID) return `session:${process.env.TERM_SESSION_ID}`;
  if (process.env.WINDOWID) return `window:${process.env.WINDOWID}`;
  return "";
}

function readWrapperSessionFile(): string {
  try {
    const filePath = path.join(os.homedir(), ".commons", "wrapper-sessions.json");
    const data = JSON.parse(fs.readFileSync(filePath, "utf-8"));
    const key = getTTYKey();
    if (!key || !data[key]) return "";
    const entry = data[key];
    // Stale check: ignore entries older than 5 minutes
    if (entry.updated_at) {
      const age = Date.now() - new Date(entry.updated_at).getTime();
      if (age > 5 * 60 * 1000) return "";
    }
    return entry.session_id || "";
  } catch {
    return "";
  }
}

main().catch((err) => {
  console.error("commons MCP server fatal error:", err);
  process.exit(1);
});
