const HOOK_SCRIPT = `${process.env.HOME}/.local/share/ai-attn/hooks/opencode.sh`;

const TRACKED_EVENTS = new Set([
  "session.status",
  "session.idle",
  "session.error",
  "session.deleted",
  "permission.asked",
  "permission.updated",
  "permission.replied",
  "message.part.updated",
]);

// message.part.updated fires on every part change while a turn runs and only
// ever reports "working", so repeats for a session within this window are
// skipped instead of starting a process each.
const HEARTBEAT_EVENT = "message.part.updated";
const HEARTBEAT_INTERVAL_MS = 1000;

const sessionOf = (event) =>
  event.properties?.sessionID ?? event.properties?.part?.sessionID ?? "";

const report = async (event) => {
  try {
    const proc = Bun.spawn(["bash", HOOK_SCRIPT], {
      stdin: "pipe",
      stdout: "ignore",
      stderr: "ignore",
    });
    proc.stdin.write(JSON.stringify(event));
    proc.stdin.end();
    await proc.exited;
  } catch {
    // Best-effort — don't crash the plugin on hook failures.
  }
};

const server = async (_ctx) => {
  // OpenCode does not await plugin event handlers, so without this queue the
  // hook processes for consecutive events run concurrently and can finish out
  // of order (a late "working" landing after the turn's idle). Each report
  // starts only after the previous one finished.
  let queue = Promise.resolve();
  const lastHeartbeat = new Map();

  return {
    event: async ({ event }) => {
      if (!TRACKED_EVENTS.has(event.type)) return;

      const session = sessionOf(event);
      if (event.type === HEARTBEAT_EVENT) {
        const now = Date.now();
        if (now - (lastHeartbeat.get(session) ?? 0) < HEARTBEAT_INTERVAL_MS) return;
        lastHeartbeat.set(session, now);
      } else {
        // Any other event changes the state, so the next heartbeat must be
        // reported even if it follows closely.
        lastHeartbeat.delete(session);
      }

      queue = queue.then(() => report(event));
      await queue;
    },
  };
};

export default {
  id: "opencode-ai-attn",
  server,
};
