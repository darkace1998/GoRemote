## 2025-02-23 - Table-driven tests for defaultAuthMethod
**Learning:** Adding comprehensive table-driven tests ensures all permutations of simple switch/if statements are validated. We noticed `TestDefaultAuthMethodTreatsMoshLikeSSH` was partially testing `defaultAuthMethod` but it lacked coverage for standard SSH and non-SSH cases, as well as proper prefix removal.
**Action:** Always favor table-driven tests for utility functions with multiple input combinations, rather than ad-hoc individual tests.

## 2024-06-15 - Optimizing Fyne Session Lookup
**Learning:** `findByConnection` was performing a linear search (O(N)) across all active tabs, which caused an unnecessary performance penalty (up to ~17,000ns per lookup in benchmarks) because sessions and tabs scale frequently.
**Action:** Replaced linear iteration with an O(1) map (`connItems map[string]*sessionTab`), initialized it at `sessionRegistry` creation, updated it correctly in the `add` and `remove` hooks, and observed lookup time drop to ~25ns.

## 2025-02-23 - Settings.json Secret Redaction Logic
**Learning:** `redactValue` function logic in `app/diagnostics/bundle.go` used exact key match equality checking, causing variants (such as myToken) to slip past the redaction filters unredacted. Using a lowercased subset match `strings.Contains(lowerK, strings.ToLower(sk))` handles prefix and suffixed secret variants successfully.
**Action:** When filtering json keys for secrets, make sure to consider prefix or suffix string modifiers that could be appended to the expected string to bypass redaction checks.
