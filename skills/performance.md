---
name: performance
description: Performance bottleneck detection
roles: [performance, optimization, efficiency]
---

## Performance Analysis

### Memory
- Unbounded caches or buffers (maps/slices that only grow)
- Large allocations in hot paths (loops, request handlers)
- Goroutine/thread leaks (spawned without cleanup)
- Loading entire files into memory when streaming is possible

### Concurrency
- Missing synchronization (race conditions)
- Lock contention (mutex held during I/O or LLM calls)
- Unnecessary serialization of parallel work

### I/O
- N+1 query patterns (loop with individual DB/API calls)
- Missing connection pooling
- Synchronous I/O where async is possible
- Large payloads without pagination or streaming

### Algorithmic
- O(n^2) or worse in loops over collections
- Repeated work that could be cached/memoized
- String concatenation in loops (use builders)
