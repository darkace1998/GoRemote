## 2025-05-18 - Optimize ID Comparison during Sorting
**Learning:** Comparing UUIDs (or generic `[16]byte` IDs) using `ID.String()` in a loop like `sort.Slice` causes immense string allocation overhead and garbage collection pressure, dramatically slowing down CPU time.
**Action:** Use `bytes.Compare(a.ID[:], b.ID[:]) < 0` to compare the underlying byte arrays directly. This achieves mathematically equivalent lexical sorting without a single heap allocation.
## 2026-06-08 - [Avoid allocations in case-insensitive search predicates]
**Learning:** In hot paths (like repeatedly evaluating nodes in a tree), `strings.Contains(strings.ToLower(s), needle)` introduces significant overhead by constantly allocating new strings via `ToLower()`.
**Action:** Since Go's `strings` package lacks a zero-allocation `ContainsFold()`, use a custom byte-level iterator that does ASCII-folding inline to avoid string allocations. Be sure to check that the strings consist of ASCII characters and fallback to the standard method otherwise to ensure correctness for all unicode characters.
## 2026-06-13 - [Improve Test Coverage for Predicate Combinators]
**Learning:** Missing unit tests for foundational logic functions like `And()` hide assumptions and edge-cases (like empty combinations or nil handling) that could surface bugs when code is refactored or extended.
**Action:** Whenever adding generic utilities or predicates, ensure tests cover basic logic rules including empty, single, and multiple inputs, as well as handling of nil arguments.
