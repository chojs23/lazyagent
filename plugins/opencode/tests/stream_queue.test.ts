import assert from "node:assert/strict";
import test from "node:test";

import {
  createPendingEventQueue,
  type QueueWriter,
} from "../src/index.ts";

class MockWriter implements QueueWriter {
  writable = true;
  writes: string[] = [];
  private readonly writeResults: boolean[];
  private readonly callbacks: Array<(error?: Error | null) => void> = [];
  private readonly drainListeners: Array<() => void> = [];

  constructor(writeResults: boolean[] = []) {
    this.writeResults = [...writeResults];
  }

  write(chunk: string, callback: (error?: Error | null) => void): boolean {
    this.writes.push(chunk);
    this.callbacks.push(callback);
    return this.writeResults.shift() ?? true;
  }

  once(event: "drain", listener: () => void): void {
    assert.equal(event, "drain");
    this.drainListeners.push(listener);
  }

  finishWrite(error?: Error | null): void {
    const callback = this.callbacks.shift();
    assert.ok(callback, "expected queued write callback");
    callback(error);
  }

  emitDrain(): void {
    const listener = this.drainListeners.shift();
    assert.ok(listener, "expected queued drain listener");
    listener();
  }
}

test("queued line stays pending until the write callback succeeds", () => {
  const writer = new MockWriter([true]);
  const queue = createPendingEventQueue({
    maxPending: 10,
    getWriter: () => writer,
  });

  assert.equal(queue.enqueue("first\n"), true);
  assert.equal(queue.pendingCount(), 1);
  assert.deepEqual(writer.writes, ["first\n"]);

  writer.finishWrite();

  assert.equal(queue.pendingCount(), 0);
});

test("new events stay ordered behind an in-flight write", () => {
  const writer = new MockWriter([true, true]);
  const queue = createPendingEventQueue({
    maxPending: 10,
    getWriter: () => writer,
  });

  queue.enqueue("first\n");
  queue.enqueue("second\n");

  assert.deepEqual(writer.writes, ["first\n"]);
  assert.equal(queue.pendingCount(), 2);

  writer.finishWrite();

  assert.deepEqual(writer.writes, ["first\n", "second\n"]);
  assert.equal(queue.pendingCount(), 1);

  writer.finishWrite();

  assert.equal(queue.pendingCount(), 0);
});

test("queue waits for drain before flushing more after backpressure", () => {
  const writer = new MockWriter([false, true]);
  const queue = createPendingEventQueue({
    maxPending: 10,
    getWriter: () => writer,
  });

  queue.enqueue("first\n");
  queue.enqueue("second\n");

  assert.deepEqual(writer.writes, ["first\n"]);

  writer.finishWrite();
  assert.deepEqual(writer.writes, ["first\n"]);
  assert.equal(queue.pendingCount(), 1);

  writer.emitDrain();
  assert.deepEqual(writer.writes, ["first\n", "second\n"]);

  writer.finishWrite();
  assert.equal(queue.pendingCount(), 0);
});

test("disconnect keeps the head item queued for retry", () => {
  let writer: MockWriter | null = new MockWriter([true]);
  const queue = createPendingEventQueue({
    maxPending: 10,
    getWriter: () => writer,
  });

  queue.enqueue("first\n");
  assert.deepEqual(writer?.writes, ["first\n"]);

  queue.markDisconnected();
  writer?.finishWrite();
  assert.equal(queue.pendingCount(), 1);

  writer = new MockWriter([true]);
  queue.drainPending();
  assert.deepEqual(writer.writes, ["first\n"]);

  writer.finishWrite();
  assert.equal(queue.pendingCount(), 0);
});

test("drain before callback still preserves order", () => {
  const writer = new MockWriter([false, true]);
  const queue = createPendingEventQueue({
    maxPending: 10,
    getWriter: () => writer,
  });

  queue.enqueue("first\n");
  queue.enqueue("second\n");

  writer.emitDrain();
  assert.deepEqual(writer.writes, ["first\n"]);
  assert.equal(queue.pendingCount(), 2);

  writer.finishWrite();
  assert.deepEqual(writer.writes, ["first\n", "second\n"]);

  writer.finishWrite();
  assert.equal(queue.pendingCount(), 0);
});

test("stale drain after disconnect does not dequeue or reorder items", () => {
  let writer: MockWriter | null = new MockWriter([false]);
  const queue = createPendingEventQueue({
    maxPending: 10,
    getWriter: () => writer,
  });

  queue.enqueue("first\n");
  queue.enqueue("second\n");

  queue.markDisconnected();
  writer.emitDrain();
  writer.finishWrite();
  assert.equal(queue.pendingCount(), 2);

  writer = new MockWriter([true, true]);
  queue.drainPending();
  assert.deepEqual(writer.writes, ["first\n"]);

  writer.finishWrite();
  assert.deepEqual(writer.writes, ["first\n", "second\n"]);

  writer.finishWrite();
  assert.equal(queue.pendingCount(), 0);
});
