import assert from "node:assert/strict";
import test from "node:test";

import { serializeToolOutput } from "../src/index.ts";

test("undefined tool output serializes to null", () => {
  assert.equal(serializeToolOutput(undefined), null);
});

test("string tool output is truncated without JSON quoting", () => {
  assert.equal(serializeToolOutput("abcdef", 3), "abc");
});

test("object tool output is JSON serialized and truncated", () => {
  assert.equal(serializeToolOutput({ ok: true }, 20), '{"ok":true}');
});

test("null tool output preserves JSON null text", () => {
  assert.equal(serializeToolOutput(null), "null");
});

test("non-serializable top-level tool outputs serialize to null", () => {
  assert.equal(serializeToolOutput(() => undefined), null);
  assert.equal(serializeToolOutput(Symbol("tool-output")), null);
});

test("throwing JSON serialization returns a truncated error string", () => {
  const circular: { self?: unknown } = {};
  circular.self = circular;

  const output = serializeToolOutput(circular, 80);

  assert.ok(output !== null);
  assert.match(output, /circular structure/i);
});
