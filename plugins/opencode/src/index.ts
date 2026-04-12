import type { Plugin } from "@opencode-ai/plugin";
import { execFileSync } from "child_process";

const LAZYAGENT_BIN = process.env.LAZYAGENT_BIN || "lazyagent";

function ingest(payload: Record<string, unknown>): void {
  try {
    execFileSync(
      LAZYAGENT_BIN,
      ["ingest", "--runtime", "opencode"],
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
  "message.updated",
  "message.part.updated",
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

    case "message.updated": {
      // info is a User or Assistant message (discriminated by role)
      const role = (info.role as string) || "";
      const result: Record<string, unknown> = {
        message_role: role,
        message_id: (info.id as string) || "",
      };
      if (role === "assistant") {
        const tokens = (info.tokens ?? {}) as Record<string, unknown>;
        const cache = (tokens.cache ?? {}) as Record<string, unknown>;
        result.cost = info.cost ?? 0;
        result.tokens_input = tokens.input ?? 0;
        result.tokens_output = tokens.output ?? 0;
        result.tokens_reasoning = tokens.reasoning ?? 0;
        result.tokens_cache_read = cache.read ?? 0;
        result.tokens_cache_write = cache.write ?? 0;
        result.finish_reason = (info.finish as string) || "";
        result.model_id = (info.modelID as string) || "";
        result.agent_name = (info.agent as string) || "";
        if (info.error) {
          const err = info.error as Record<string, unknown>;
          result.error_name = (err.name as string) || "";
          result.error_message = (err.message as string) || "";
        }
      }
      return result;
    }

    case "message.part.updated": {
      const part = (props.part ?? {}) as Record<string, unknown>;
      const partType = (part.type as string) || "";
      const result: Record<string, unknown> = {
        part_type: partType,
        part_id: (part.id as string) || "",
        message_id: (part.messageID as string) || "",
      };
      switch (partType) {
        case "text":
          // Truncate text to 10KB to avoid bloating the payload
          result.text = typeof part.text === "string"
            ? (part.text as string).slice(0, 10000)
            : "";
          break;
        case "reasoning":
          result.text = typeof part.text === "string"
            ? (part.text as string).slice(0, 10000)
            : "";
          break;
        case "tool": {
          result.tool_name = (part.tool as string) || "";
          result.call_id = (part.callID as string) || "";
          const state = (part.state ?? {}) as Record<string, unknown>;
          result.tool_status = (state.status as string) || "";
          result.tool_title = (state.title as string) || "";
          if (state.status === "error") {
            result.tool_error = (state.error as string) || "";
          }
          break;
        }
        case "step-finish": {
          result.finish_reason = (part.reason as string) || "";
          result.cost = part.cost ?? 0;
          const tokens = (part.tokens ?? {}) as Record<string, unknown>;
          const cache = (tokens.cache ?? {}) as Record<string, unknown>;
          result.tokens_input = tokens.input ?? 0;
          result.tokens_output = tokens.output ?? 0;
          result.tokens_reasoning = tokens.reasoning ?? 0;
          result.tokens_cache_read = cache.read ?? 0;
          result.tokens_cache_write = cache.write ?? 0;
          break;
        }
      }
      return result;
    }

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
