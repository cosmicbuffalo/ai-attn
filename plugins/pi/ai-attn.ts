// ai-attn extension for Pi (managed by ai-attn: `ai-attn setup pi` rewrites
// this file, so edits are lost). It reports each lifecycle event to
// `ai-attn hook --agent pi`, which maps events to attention states.
//
// Needs Pi 0.80.5+ for agent_settled; waiting (ui_prompt_*) needs 0.84.4+.

import { spawn } from "node:child_process";

const AGENT = "pi";
const HOOK_TIMEOUT_MS = 5000;

export default function (pi: any) {
  let sessionId = "";
  let cwd = process.cwd();
  // Pi does not await every handler, so writes are chained to keep an older
  // event from landing after a newer one.
  let writes: Promise<void> = Promise.resolve();

  function remember(ctx: any): void {
    try {
      const id = ctx?.sessionManager?.getSessionId?.();
      if (id) sessionId = String(id);
    } catch {
      // Keep the last known id.
    }
    if (ctx?.cwd) cwd = String(ctx.cwd);
  }

  function runHook(payload: string): Promise<void> {
    return new Promise((resolve) => {
      try {
        const child = spawn(process.env.AI_ATTN_BIN || "ai-attn", ["hook", "--agent", AGENT], {
          stdio: ["pipe", "ignore", "ignore"],
        });
        const timer = setTimeout(() => child.kill(), HOOK_TIMEOUT_MS);
        const done = () => {
          clearTimeout(timer);
          resolve();
        };
        child.on("error", done);
        child.on("close", done);
        child.stdin.on("error", () => {});
        child.stdin.end(payload);
      } catch {
        resolve();
      }
    });
  }

  function report(event: string, ctx: any, fields: Record<string, unknown> = {}): Promise<void> {
    remember(ctx);
    const payload = JSON.stringify({ event, session_id: sessionId, cwd, ...fields });
    writes = writes.then(() => runHook(payload));
    return writes;
  }

  pi.on("session_start", (_event: any, ctx: any) => {
    remember(ctx);
  });
  pi.on("agent_start", (_event: any, ctx: any) => {
    void report("agent_start", ctx);
  });
  pi.on("ui_prompt_start", (_event: any, ctx: any) => {
    void report("ui_prompt_start", ctx);
  });
  pi.on("ui_prompt_end", (_event: any, ctx: any) => {
    void report("ui_prompt_end", ctx);
  });
  pi.on("agent_settled", (event: any, ctx: any) => {
    void report("agent_settled", ctx, { aborted: Boolean(event?.aborted) });
  });
  // Awaited so the write finishes before Pi exits.
  pi.on("session_shutdown", async (event: any, ctx: any) => {
    await report("session_shutdown", ctx, { reason: event?.reason ?? "" });
  });
}
