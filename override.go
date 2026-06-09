// Copyright 2026 Suyono
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package confstruct

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
)

// Override returns a writable watchable backend intended for user-supplied
// overrides. Register it as a high-precedence layer and keep the returned
// backend so user code can call Set or Unset to force a re-resolution.
func Override(values map[string]any) *OverrideBackend {
	m := make(map[string]any, len(values))
	maps.Copy(m, values)
	return &OverrideBackend{
		values:   m,
		watchers: make(map[string][]overrideWatcher),
	}
}

type overrideWatcher struct {
	id   uint64
	ctx  context.Context
	hook func(any, bool)
}

// OverrideBackend is a writable watchable backend for user-controlled
// overrides. Calling Set or Unset notifies any registered watchers immediately.
type OverrideBackend struct {
	mu            sync.RWMutex
	values        map[string]any
	watchers      map[string][]overrideWatcher
	nextWatcherID atomic.Uint64
}

// OverrideBackendName is the Name() identifier for an [Override] backend.
// Use it to compare against [ResolveHook] arguments without hard-coding the string.
const OverrideBackendName = "override"

func (b *OverrideBackend) Lookup(path string) (any, bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	v, ok := b.values[path]
	return v, ok, nil
}

func (b *OverrideBackend) Watch(ctx context.Context, path string, hook func(v any, ok bool)) {
	if ctx.Err() != nil {
		return
	}

	w := overrideWatcher{
		id:   b.nextWatcherID.Add(1),
		ctx:  ctx,
		hook: hook,
	}

	b.mu.Lock()
	b.watchers[path] = append(b.watchers[path], w)
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.removeWatcher(path, w.id)
	}()
}

// Set stores a user-supplied override for path and triggers re-resolution of
// the corresponding entry.
func (b *OverrideBackend) Set(path string, value any) {
	watchers := b.update(path, value, true)
	for _, w := range watchers {
		if w.ctx.Err() == nil {
			w.hook(value, true)
		}
	}
}

// Unset removes the user-supplied override for path and triggers re-resolution
// so lower-priority layers can win again.
func (b *OverrideBackend) Unset(path string) {
	watchers := b.update(path, nil, false)
	for _, w := range watchers {
		if w.ctx.Err() == nil {
			w.hook(nil, false)
		}
	}
}

func (b *OverrideBackend) Name() string { return OverrideBackendName }

func (b *OverrideBackend) Describe() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return fmt.Sprintf("%d keys", len(b.values))
}

func (b *OverrideBackend) update(path string, value any, ok bool) []overrideWatcher {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ok {
		b.values[path] = value
	} else {
		delete(b.values, path)
	}

	watchers := append([]overrideWatcher(nil), b.watchers[path]...)
	return watchers
}

func (b *OverrideBackend) removeWatcher(path string, id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	watchers := b.watchers[path]
	for i, w := range watchers {
		if w.id != id {
			continue
		}
		watchers = append(watchers[:i], watchers[i+1:]...)
		if len(watchers) == 0 {
			delete(b.watchers, path)
		} else {
			b.watchers[path] = watchers
		}
		return
	}
}
