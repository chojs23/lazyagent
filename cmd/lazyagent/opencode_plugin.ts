import type { Plugin } from "@opencode-ai/plugin";
import { execFile } from "child_process";

const LAZYAGENT_BIN = process.env.LAZYAGENT_BIN || "lazyagent";
const PROJECT_SLUG = process.env.LAZYAGENT_PROJECT_SLUG || "";

function ingest(payload: Record<string, unknown>): void {
  const child = execFile(
    LAZYAGENT_BIN,
    [
      "ingest",
      "--runtime",
      "opencode",
      ...(PROJECT_SLUG ? ["--project-slug", PROJECT_SLUG] : []),
    ],
    { timeout: 5000 },
    (err) => {
      if (err) {
        console.error("[lazyagent] ingest error:", err.message);
      }
    }
  );
  if (child.stdin) {
    child.stdin.write(JSON.stringify(payload));
    child.stdin.end();
  }
}

export const LazyagentPlugin: Plugin = async ({ project }) => {
  const projectDir = project?.path || process.cwd();

  return {
    "tool.execute.before": async ({ tool, sessionID, callID }, { args }) => {
      ingest({
        event: "tool.execute.before",
        session_id: sessionID,
        tool,
        call_id: callID,
        args,
        project_dir: projectDir,
        timestamp: Date.now(),
      });
    },

    "tool.execute.after": async (
      { tool, sessionID, callID },
      { title, output, metadata }
    ) => {
      ingest({
        event: "tool.execute.after",
        session_id: sessionID,
        tool,
        call_id: callID,
        title,
        output:
          typeof output === "string"
            ? output.slice(0, 10000)
            : JSON.stringify(output).slice(0, 10000),
        metadata,
        project_dir: projectDir,
        timestamp: Date.now(),
      });
    },

    event: async ({ event }: { event: { type: string; [key: string]: unknown } }) => {
      const type = event.type;

      if (
        !type.startsWith("session.") &&
        type !== "permission.asked"
      ) {
        return;
      }

      if (type === "tool.execute.before" || type === "tool.execute.after") {
        return;
      }

      ingest({
        event: type,
        session_id: (event as any).sessionID || (event as any).session_id || "",
        project_dir: projectDir,
        timestamp: Date.now(),
        ...event,
      });
    },
  };
};
