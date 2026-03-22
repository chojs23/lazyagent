import type { Plugin } from "@opencode-ai/plugin";
import { execFileSync } from "child_process";

const LAZYAGENT_BIN = process.env.LAZYAGENT_BIN || "lazyagent";
const PROJECT_SLUG = process.env.LAZYAGENT_PROJECT_SLUG || "";

function ingest(payload: Record<string, unknown>): void {
  try {
    execFileSync(
      LAZYAGENT_BIN,
      [
        "ingest",
        "--runtime",
        "opencode",
        ...(PROJECT_SLUG ? ["--project-slug", PROJECT_SLUG] : []),
      ],
      {
        input: JSON.stringify(payload),
        timeout: 5000,
        stdio: ["pipe", "pipe", "pipe"],
      }
    );
  } catch (err: any) {
    console.error("[lazyagent] ingest error:", err.message);
  }
}

function extractSessionID(event: Record<string, unknown>): string {
  const props = (event.properties ?? {}) as Record<string, unknown>;
  const type = event.type as string;

  // session.created / session.deleted: properties.info.id
  if (type === "session.created" || type === "session.deleted") {
    const info = props.info as Record<string, unknown> | undefined;
    return (info?.id as string) || "";
  }

  // session.idle: properties.sessionID
  if (type === "session.idle") {
    return (props.sessionID as string) || "";
  }

  // fallback: try common locations
  return (
    (props.sessionID as string) ||
    (props.session_id as string) ||
    (event.sessionID as string) ||
    (event.session_id as string) ||
    ""
  );
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

    event: async ({ event }: { event: Record<string, unknown> }) => {
      const type = event.type as string;

      if (
        !type.startsWith("session.") &&
        type !== "permission.asked"
      ) {
        return;
      }

      if (type === "tool.execute.before" || type === "tool.execute.after") {
        return;
      }

      const sessionID = extractSessionID(event);
      if (!sessionID) {
        return; // skip events without identifiable session
      }

      const props = (event.properties ?? {}) as Record<string, unknown>;
      const info = (props.info ?? {}) as Record<string, unknown>;

      ingest({
        event: type,
        session_id: sessionID,
        parent_session_id: (info.parentID as string) || "",
        project_dir: projectDir,
        timestamp: Date.now(),
      });
    },
  };
};
