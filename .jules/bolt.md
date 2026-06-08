## 2025-05-18 - Optimize ID Comparison during Sorting
**Learning:** Comparing UUIDs (or generic `[16]byte` IDs) using `ID.String()` in a loop like `sort.Slice` causes immense string allocation overhead and garbage collection pressure, dramatically slowing down CPU time.
**Action:** Use `bytes.Compare(a.ID[:], b.ID[:]) < 0` to compare the underlying byte arrays directly. This achieves mathematically equivalent lexical sorting without a single heap allocation.
