## 2025-02-23 - Table-driven tests for defaultAuthMethod
**Learning:** Adding comprehensive table-driven tests ensures all permutations of simple switch/if statements are validated. We noticed `TestDefaultAuthMethodTreatsMoshLikeSSH` was partially testing `defaultAuthMethod` but it lacked coverage for standard SSH and non-SSH cases, as well as proper prefix removal.
**Action:** Always favor table-driven tests for utility functions with multiple input combinations, rather than ad-hoc individual tests.

## 2025-02-23 - Centralize Redaction Logic
**Learning:** Incomplete redaction of sensitive settings occurred because manual substring matching was used instead of the centralized `redactValue` utility function with a comprehensive `SecretKeys` list.
**Action:** Always prefer reusing existing security utility functions like `redactValue` for redacting fields over ad-hoc key checks.
