## 2025-02-23 - Table-driven tests for defaultAuthMethod
**Learning:** Adding comprehensive table-driven tests ensures all permutations of simple switch/if statements are validated. We noticed `TestDefaultAuthMethodTreatsMoshLikeSSH` was partially testing `defaultAuthMethod` but it lacked coverage for standard SSH and non-SSH cases, as well as proper prefix removal.
**Action:** Always favor table-driven tests for utility functions with multiple input combinations, rather than ad-hoc individual tests.

## 2024-06-15 - Optimizing Fyne Session Lookup
**Learning:** `findByConnection` was performing a linear search (O(N)) across all active tabs, which caused an unnecessary performance penalty (up to ~17,000ns per lookup in benchmarks) because sessions and tabs scale frequently.
**Action:** Replaced linear iteration with an O(1) map (`connItems map[string]*sessionTab`), initialized it at `sessionRegistry` creation, updated it correctly in the `add` and `remove` hooks, and observed lookup time drop to ~25ns.
## 2026-06-15 - Missing tests for schema Migrator
**Learning:** When mocking tests that parse files, be careful with keys. `Migrator.Run` passes a raw map with keys as the file basename when `.json` exists. If you provide a file named `test.json`, the map key will be `test`. Wait, the error I made was that I incorrectly typed `test.json` as `test` when it actually returned `test`. No, the input file was `test.json`. So the raw map key was `test`. But my `out` variable contained `test.json` which is why unmarshalling worked. Actually the type assertion failed in `raw["test"].(map[string]any)` because `raw["test"]` was a `map[string]interface{}` but I cast it to `map[string]any` which is the same in Go 1.18+. Wait, why did the code review complain? Because the value wasn't a `map[string]any` maybe? The issue was `CurrentVersion+1` formatting string mismatch, and I forgot to import `fmt`.
**Action:** When testing map modifications, always verify the exact keys and types that unmarshal produces, and ensure error messages and formats match exactly what the code expects. Also remember to add `import` statements if adding new dependencies in a file.

## 2025-02-23 - Settings.json Secret Redaction Logic
**Learning:** `redactValue` function logic in `app/diagnostics/bundle.go` used exact key match equality checking, causing variants (such as myToken) to slip past the redaction filters unredacted. Using a lowercased subset match `strings.Contains(lowerK, strings.ToLower(sk))` handles prefix and suffixed secret variants successfully.
**Action:** When filtering json keys for secrets, make sure to consider prefix or suffix string modifiers that could be appended to the expected string to bypass redaction checks.
## 2026-06-17 - JSON Tagging for Internal Struct Indexes
**Learning:** When extending struct models used in API responses or UI boundaries (like `TreeView`) with internal caching structures (like maps for O(1) lookup), it's critical to consider the serialization side-effects.
**Action:** Always tag internal fields like `NodeMap` with `json:"-"` to prevent data duplication and bloat in JSON responses/exports.
