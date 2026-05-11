## 2024-05-15 - Double Allocations in Nested Appends
**Learning:** Using `append(append([]byte(nil), data...), newByte)` causes two allocations because the inner append allocates exactly `len(data)` and the outer append triggers a slice growth reallocation.
**Action:** Use `make([]byte, 0, len(data) + len(newBytes))` followed by sequential appends to ensure exactly one allocation occurs when modifying and cloning slices.
