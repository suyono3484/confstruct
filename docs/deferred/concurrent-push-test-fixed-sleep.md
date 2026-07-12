# Deferred: replace fixed `time.Sleep(50ms)` in the concurrent-push regression test

**Status:** Won't fix for now — deferred, not abandoned.
**Where:** `confstruct_test.go`, `TestOnResolve_NotFiredOnConcurrentPushDuringFailedPopulate`.

## Background: the race this test guards against

`Populate` walks a config struct field by field. For each field backed by a
`WatchableBackend` (a backend that can push live updates after the initial
read, such as `Override`), it registers a watch via `wb.Watch(ctx, key, ...)`
as soon as that field is processed — not after the whole struct walk
finishes. That watch's push closure looks like this today
(`confstruct.go`, inside `walkAndInject`):

```go
wb.Watch(ctx, key, func(v any, ok bool) {
	// Live-update coercion failures are intentionally discarded:
	// Populate has already returned, so there's no error path to
	// report through. Entry keeps its last good value.
	_ = lm.setSlot(idx, v, ok)
	// A push can arrive while this Populate call is still walking
	// later fields (e.g. a concurrent Override.Set from another
	// goroutine), before success/failure is known. Only notify once
	// Populate has actually reached stateDone: notifying here on a
	// call that goes on to fail would fire OnResolve for a struct
	// Populate reports as not populated. If Populate does succeed,
	// this field's entry in the pending-notify flush below picks up
	// the pushed value (hasChangedSinceNotify compares state, not
	// events, so nothing is lost by skipping this one).
	if meta.state.Load() == stateDone {
		notify()
	}
})
```

The `if meta.state.Load() == stateDone` guard exists to close a real race:
because watches go live progressively as the walk proceeds, a caller that
holds a registered `WatchableBackend` (e.g. `*OverrideBackend`) can call
`Set` on an *already-watched, earlier* field from another goroutine while
`Populate` is still resolving a *later* field. Without the guard, that push
would fire `OnResolve` (via `notify()`) immediately — even though `Populate`
might go on to fail and return a non-nil error for the struct as a whole.
`meta.state` is an `atomic.Uint32` on `Meta` that only ever moves
`stateIdle → stateRunning → stateDone` on success, or back to `stateIdle` on
failure (it never reverts out of `stateDone` once reached), so gating the
push's `notify()` on `stateDone` means a push that lands mid-walk is simply
skipped — the field's value is still updated via `setSlot`, but `OnResolve`
only fires later, either because `Populate` succeeds and flushes its own
pending notifies, or not at all if `Populate` fails.

## The regression test

`TestOnResolve_NotFiredOnConcurrentPushDuringFailedPopulate` reproduces this
race directly. It uses a `testConfig` (defined in `confstruct_test.go`) with
a `Name StringEntry` field and a `Port IntEntry` field, plus a purpose-built
fixture:

```go
type blockingBackend struct {
	field   string
	release chan struct{}
}

func (b *blockingBackend) Lookup(path string) (any, bool, error) {
	if path != b.field {
		return nil, false, nil
	}
	<-b.release
	return "not-a-port", true, nil
}

func (b *blockingBackend) Name() string     { return "blocking" }
func (b *blockingBackend) Describe() string { return "" }
```

`blockingBackend` blocks in `Lookup` until its `release` channel is closed,
which lets the test control exactly when `Populate`'s walk is allowed to
reach and fail on `Port` (the value `"not-a-port"` fails `int` coercion).

The test itself:

```go
func TestOnResolve_NotFiredOnConcurrentPushDuringFailedPopulate(t *testing.T) {
	var cfg testConfig
	var count atomic.Int64
	cfg.OnResolve(func(key string, value any, backendName, backendDesc string) {
		count.Add(1)
	})

	cfg.AddLayer(Map(map[string]any{"Name": "default", "Port": 0}))
	ob := Override(map[string]any{"Name": "initial"})
	cfg.AddLayer(ob)

	release := make(chan struct{})
	cfg.AddLayer(&blockingBackend{field: "Port", release: release})

	var wg sync.WaitGroup
	wg.Add(1)
	var populateErr error
	go func() {
		defer wg.Done()
		populateErr = Populate(context.Background(), &cfg)
	}()

	// give Populate time to register Name's Override watch and block on Port's Lookup
	time.Sleep(50 * time.Millisecond)
	ob.Set("Name", "concurrent-update")
	close(release)
	wg.Wait()

	if populateErr == nil {
		t.Fatal("expected Populate to fail due to Port's bad value")
	}
	if got := count.Load(); got != 0 {
		t.Fatalf("OnResolve fire count during/before a failed Populate call: got %d, want 0 (err=%v)", got, populateErr)
	}
}
```

`Populate` runs on its own goroutine. Because `testConfig` declares `Name`
before `Port`, the walk processes and watches `Name` (backed by the
`Override` `ob`) before it reaches `Port` (backed by the blocking fixture).
The test needs `Populate` to have already registered `Name`'s watch, and to
be currently blocked in `blockingBackend.Lookup`, before it calls
`ob.Set("Name", "concurrent-update")` — otherwise it isn't exercising the
race at all.

## Issue description

The test gets to that precondition with a blind
`time.Sleep(50 * time.Millisecond)` between starting `Populate` on a
goroutine and calling `ob.Set`. This is a timing guess, not a
synchronization guarantee.

## Why this matters (failure scenario)

On a loaded or CPU-constrained CI runner (many parallel `go test -race`
binaries, low CPU allocation, a constrained `GOMAXPROCS`), the `Populate`
goroutine may not be scheduled onto a thread within 50ms. If
`ob.Set("Name", "concurrent-update")` executes **before**
`wb.Watch(ctx, "Name", ...)` has registered its closure, then:

- `OverrideBackend.Set` (in `override.go`) looks up registered watchers for
  `"Name"` under its own lock and finds none yet, so the push hook never
  fires at all.
- The test still passes — `count.Load()` is `0`, matching the assertion —
  but for the wrong reason. It never actually exercised the
  `meta.state.Load() == stateDone` guard described above.

That makes this test a **silent false negative under load**: a future
regression that reintroduces the unconditional `notify()` (e.g. someone
"simplifying" the closure back to its pre-guard form, or refactoring the
`stateDone` check away) could pass this test undetected on a slow or
contended machine, while still failing it on a fast one. That's worse than
an ordinary flaky test, because it fails in the direction of "looks green,
isn't testing anything" rather than "fails spuriously."

## Existing pattern to reuse

The same file already solves this class of problem deterministically in
`TestPopulate_failureCancelsWatches`, which polls `OverrideBackend`'s
internal watcher list under its exported-for-tests lock, against a bounded
deadline, instead of sleeping a fixed duration (note: that particular test
polls for a watcher list to become *empty*, proving cleanup ran — the
pattern to borrow is the poll-with-deadline structure, not that exact
condition):

```go
deadline := time.Now().Add(time.Second)
for {
	ov.mu.RLock()
	n := len(ov.watchers["Name"])
	ov.mu.RUnlock()
	if n == 0 {
		break
	}
	if time.Now().After(deadline) {
		t.Fatalf("watcher for Name still registered (%d) after failed Populate", n)
	}
	time.Sleep(time.Millisecond)
}
```

## Suggested fix (deferred)

Replace the fixed sleep with a poll-until-condition loop, bounded by a
deadline, matching that idiom. The concrete condition to poll for is "Name's
Override watch has been registered" (the inverse of the example above — poll
until `len(ov.watchers["Name"]) > 0`, or equivalent), or:

Add a small synchronization hook to `blockingBackend` itself — e.g. a
channel that's closed the instant `Lookup` is entered — and wait on that
instead of inferring "Populate must have registered Name's watch by now"
indirectly from "Port's Lookup must have been entered by now." This is
arguably more direct evidence of the precondition the test actually needs,
since walk order already guarantees `Name` is processed (hence watched)
before `Port` is reached.

Either approach removes the fixed-duration guess and makes the test's pass
result actually contingent on having exercised the race it's named for.

## Why deferred rather than fixed now

Low priority relative to other planned work: the test is not flaky in
practice on the machines it's been run on (`go test -race -count=20` passed
cleanly), and the risk is a silent coverage gap under unusual load, not an
active bug. Revisit alongside other test-hygiene cleanup, or sooner if this
test is observed to flake in CI.
