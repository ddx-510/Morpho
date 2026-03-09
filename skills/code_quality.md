---
name: code_quality
description: Code quality and maintainability analysis
roles: [quality, maintainability, architecture, reviewer]
---

## Code Quality Analysis

### Error Handling
- Unchecked errors (Go: `err` ignored, Python: bare except)
- Error swallowing (catch-and-ignore patterns)
- Missing error context (wrapping without message)

### Code Smells
- Functions longer than 50 lines
- Deep nesting (>3 levels)
- God objects / classes with too many responsibilities
- Duplicate code blocks
- Magic numbers without named constants

### Architecture
- Circular dependencies between packages
- Layer violations (UI importing data layer directly)
- Missing interfaces at boundaries
- Tight coupling between modules

### Testing
- Missing test files for critical paths
- No error case testing
- Test files that only test happy paths

### Patterns to check
```
# Find long functions
grep -n "^func " *.go | head -20
# Find TODO/FIXME
grep -rn "TODO\|FIXME\|HACK\|XXX" .
# Find unused exports
grep -rn "^func [A-Z]" *.go
```
