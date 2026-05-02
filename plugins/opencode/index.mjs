const HOOK_SCRIPT = `${process.env.HOME}/.local/share/ai-attn/hooks/opencode.sh`;

const TRACKED_EVENTS = new Set([
  "session.status",
  "session.idle",
  "session.error",
  "session.created",
  "session.deleted",
  "permission.updated",
  "permission.replied",
  "message.part.updated",
  "file.edited",
]);

const server = async (_ctx) => {
  return {
    event: async ({ event }) => {
      if (!TRACKED_EVENTS.has(event.type)) return;

      const json = JSON.stringify(event);
      try {
        const proc = Bun.spawn(["bash", HOOK_SCRIPT], {
          stdin: "pipe",
          stdout: "ignore",
          stderr: "ignore",
        });
        proc.stdin.write(json);
        proc.stdin.end();
        await proc.exited;
      } catch {
        // Best-effort — don't crash the plugin on hook failures.
      }
    },
  };
};

export default {
  id: "opencode-ai-attn",
  server,
};
