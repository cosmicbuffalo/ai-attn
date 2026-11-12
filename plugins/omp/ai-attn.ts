// ai-attn extension for Oh My Pi (managed by ai-attn: `ai-attn setup omp`
// rewrites this file, so edits are lost). It reports each lifecycle event to
// `ai-attn hook --agent omp`, which maps events to attention states.

import { spawn } from "node:child_process";

const AGENT = "omp";
const HOOK_TIMEOUT_MS = 5000;

export default function (pi: any) {
  let sessionId = "";
  let cwd = process.cwd();
  // omp fires agent_end without awaiting its handlers, so writes are chained
  // to keep an older event from landing after a newer one.
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
  pi.on("tool_approval_requested", (event: any, ctx: any) => {
    void report("tool_approval_requested", ctx, { tool_name: event?.toolName ?? "" });
  });
  pi.on("tool_approval_resolved", (_event: any, ctx: any) => {
    void report("tool_approval_resolved", ctx);
  });
  // Only the ask tool changes state: it blocks the turn on the user.
  pi.on("tool_execution_start", (event: any, ctx: any) => {
    if (event?.toolName === "ask") void report("tool_execution_start", ctx, { tool_name: "ask" });
  });
  pi.on("tool_execution_end", (event: any, ctx: any) => {
    if (event?.toolName === "ask") void report("tool_execution_end", ctx, { tool_name: "ask" });
  });
  pi.on("agent_end", (event: any, ctx: any) => {
    void report("agent_end", ctx, { will_continue: event?.willContinue === true });
  });
  // Awaited so the write finishes within omp's shutdown budget.
  pi.on("session_shutdown", async (_event: any, ctx: any) => {
    await report("session_shutdown", ctx);
  });
}
