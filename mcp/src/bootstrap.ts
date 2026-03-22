interface SlotContext {
  previous_sessions: number;
  last_session?: {
    ended_at: string;
    end_reason: string;
    last_cwd: string;
    last_branch?: string;
    last_state_detail?: string;
  };
  pending_messages?: Array<{
    id: string;
    from_slot: string;
    content: string;
    sent_at: string;
  }>;
}

export function formatBootstrap(slotName: string, sessionNumber: number, ctx: SlotContext): string {
  const lines: string[] = [];
  lines.push(`[commons] Reconnected to slot ${slotName} (session #${sessionNumber})`);

  if (ctx.last_session) {
    const ls = ctx.last_session;
    const endedAt = ls.ended_at ? formatTimeAgo(ls.ended_at) : "unknown";
    lines.push(`  Previous session ended ${endedAt} (${ls.end_reason || "unknown"})`);
    if (ls.last_cwd) {
      lines.push(`  Last working directory: ${ls.last_cwd}`);
    }
  }

  const pendingCount = ctx.pending_messages?.length || 0;
  lines.push(`  Pending messages: ${pendingCount}`);

  if (ctx.pending_messages) {
    for (const msg of ctx.pending_messages) {
      const ago = formatTimeAgo(msg.sent_at);
      const from = msg.from_slot || "system";
      lines.push(`    @${from}: "${truncate(msg.content, 80)}" (${ago})`);
    }
  }

  return lines.join("\n");
}

function formatTimeAgo(isoDate: string): string {
  const then = new Date(isoDate).getTime();
  const now = Date.now();
  const diffMs = now - then;

  if (diffMs < 60000) return `${Math.round(diffMs / 1000)}s ago`;
  if (diffMs < 3600000) return `${Math.round(diffMs / 60000)}m ago`;
  if (diffMs < 86400000) return `${Math.round(diffMs / 3600000)}h ago`;
  return `${Math.round(diffMs / 86400000)}d ago`;
}

function truncate(s: string, n: number): string {
  return s.length <= n ? s : s.slice(0, n) + "...";
}
