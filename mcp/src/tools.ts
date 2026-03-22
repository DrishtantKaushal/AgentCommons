import { DaemonClient } from "./daemon-client.js";

interface SlotInfo {
  slot_id: string;
  slot_name: string;
  last_state: string;
  last_state_detail?: string;
  last_cwd?: string;
  total_sessions: number;
  active_session?: {
    session_id: string;
    agent_type: string;
    pid: number;
    cwd: string;
    state: string;
    state_detail?: string;
    started_at: string;
    last_heartbeat: string;
  };
}

export async function handleStatus(client: DaemonClient): Promise<string> {
  if (!client.isConnected) {
    return "Not connected to commons daemon";
  }

  try {
    const resp = await client.send("list_agents", { filter: "all" });
    const agents: SlotInfo[] = resp.payload.agents || [];

    if (agents.length === 0) {
      return "No agents registered";
    }

    const alive = agents.filter((a) => a.active_session).length;
    const inactive = agents.length - alive;

    let out = `AGENTS (${alive} alive`;
    if (inactive > 0) out += `, ${inactive} inactive`;
    out += ")\n";

    for (const agent of agents) {
      if (agent.active_session) {
        const s = agent.active_session;
        const state = s.state;
        let line = `  ${agent.slot_name}   ${s.agent_type}  ${state}   ${s.cwd}`;
        out += line + "\n";

        if (state === "blocked_on_approval" && s.state_detail) {
          out += `    Awaiting: "${s.state_detail}"\n`;
          out += `    /approve @${agent.slot_name}\n`;
        }
      } else {
        const lastSeen = agent.last_cwd || "unknown";
        out += `  ${agent.slot_name}   inactive   last cwd: ${lastSeen}\n`;
      }
    }

    return out.trimEnd();
  } catch (err) {
    return `Error fetching status: ${err}`;
  }
}

export class NotificationQueue {
  private queue: string[] = [];

  push(notification: string): void {
    this.queue.push(notification);
  }

  drain(): string[] {
    const items = this.queue.splice(0);
    return items;
  }

  get length(): number {
    return this.queue.length;
  }
}

export function handleInbox(notifications: NotificationQueue): string {
  const items = notifications.drain();
  if (items.length === 0) {
    return "Inbox empty — messages are delivered live via channel push";
  }
  return items.join("\n\n");
}

interface MessageHistoryItem {
  id: string;
  from_name: string;
  to_name: string;
  content: string;
  type: string;
  status: string;
  created_at: string;
}

export async function handleHistory(
  client: DaemonClient,
  limit: number = 20
): Promise<string> {
  if (!client.isConnected) {
    return "Not connected to commons daemon";
  }

  try {
    const resp = await client.send("list_messages", { limit });
    const messages: MessageHistoryItem[] = resp.payload.messages || [];

    if (messages.length === 0) {
      return "No messages yet";
    }

    let out = `MESSAGE HISTORY (${messages.length} messages)\n\n`;

    // Messages come in DESC order; show oldest first
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i];
      const ts = formatTimestamp(m.created_at);
      const typeLabel = m.type === "task" ? "task" : "msg";
      out += `  ${ts}  ${typeLabel}  #${m.from_name} -> #${m.to_name}  [${m.status}]\n`;
      out += `    ${m.content}\n\n`;
    }

    return out.trimEnd();
  } catch (err) {
    return `Error fetching history: ${err}`;
  }
}

function formatTimestamp(ts: string): string {
  try {
    const d = new Date(ts);
    if (isNaN(d.getTime())) return ts;
    return d.toLocaleString("en-US", {
      month: "short",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    });
  } catch {
    return ts;
  }
}

export async function handleReportState(
  client: DaemonClient,
  sessionId: string,
  state: string,
  stateDetail?: string
): Promise<string> {
  if (!client.isConnected) {
    return "Not connected to commons daemon";
  }

  client.sendFire("heartbeat", {
    session_id: sessionId,
    state,
    state_detail: stateDetail || "",
    cwd: process.cwd(),
  });

  return `State reported: ${state}`;
}
