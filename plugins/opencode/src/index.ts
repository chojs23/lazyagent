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

// Events we forward from the generic event hook.
// Tool events are handled by their own dedicated hooks.
const FORWARDED_EVENTS = new Set([
  "session.created",
  "session.updated",
  "session.deleted",
  "session.idle",
  "session.status",
  "session.diff",
  "session.error",
  "session.compacted",
  "permission.asked",
  "permission.replied",
  "todo.updated",
  "command.executed",
  "file.edited",
]);

function extractSessionID(event: Record<string, unknown>): string {
  const props = (event.properties ?? {}) as Record<string, unknown>;
  const type = event.type as string;

  // session.created / session.deleted / session.updated: properties.info.id
  if (
    type === "session.created" ||
    type === "session.deleted" ||
    type === "session.updated"
  ) {
    const info = props.info as Record<string, unknown> | undefined;
    return (info?.id as string) || "";
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

// Extract event-specific payload fields from properties.
function extractEventData(
  type: string,
  props: Record<string, unknown>
): Record<string, unknown> {
  const info = (props.info ?? {}) as Record<string, unknown>;

  switch (type) {
    case "session.created":
    case "session.deleted":
    case "session.updated":
      return {
        parent_session_id: (info.parentID as string) || "",
        title: (info.title as string) || "",
      };

    case "session.status": {
      const status = (props.status ?? {}) as Record<string, unknown>;
      return {
        status_type: (status.type as string) || "",
        // retry fields (only present when status.type === "retry")
        retry_attempt: status.attempt ?? null,
        retry_message: (status.message as string) || "",
        retry_next: status.next ?? null,
      };
    }

    case "session.diff": {
      const diffs = (props.diff ?? []) as Array<Record<string, unknown>>;
      let additions = 0;
      let deletions = 0;
      for (const d of diffs) {
        additions += (d.additions as number) || 0;
        deletions += (d.deletions as number) || 0;
      }
      return {
        diff_file_count: diffs.length,
        diff_additions: additions,
        diff_deletions: deletions,
      };
    }

    case "session.error": {
      const error = (props.error ?? {}) as Record<string, unknown>;
      return {
        error_type: (error.type as string) || "",
        error_message: (error.message as string) || "",
      };
    }

    case "permission.asked":
      return {
        permission: (props.permission as string) || "",
        patterns: props.patterns ?? [],
      };

    case "permission.replied":
      return {
        reply: (props.reply as string) || "",
      };

    case "todo.updated": {
      const todos = (props.todos ?? []) as Array<Record<string, unknown>>;
      return {
        todo_count: todos.length,
        todos: todos.map((t) => ({
          content: t.content,
          status: t.status,
          priority: t.priority,
        })),
      };
    }

    case "command.executed":
      return {
        command_name: (props.name as string) || "",
        command_args: (props.arguments as string) || "",
      };

    case "file.edited":
      return {
        file: (props.file as string) || "",
      };

    default:
      return {
        parent_session_id: (info.parentID as string) || "",
        title: (info.title as string) || "",
      };
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

    event: async ({ event }: { event: Record<string, unknown> }) => {
      const type = event.type as string;

      if (!FORWARDED_EVENTS.has(type)) {
        return;
      }

      const sessionID = extractSessionID(event);
      if (!sessionID) {
        return;
      }

      const props = (event.properties ?? {}) as Record<string, unknown>;
      const eventData = extractEventData(type, props);

      ingest({
        event: type,
        session_id: sessionID,
        project_dir: projectDir,
        timestamp: Date.now(),
        ...eventData,
      });
    },
  };
};
