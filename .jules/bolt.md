## 2025-02-23 - Table-driven tests for defaultAuthMethod
**Learning:** Adding comprehensive table-driven tests ensures all permutations of simple switch/if statements are validated. We noticed `TestDefaultAuthMethodTreatsMoshLikeSSH` was partially testing `defaultAuthMethod` but it lacked coverage for standard SSH and non-SSH cases, as well as proper prefix removal.
**Action:** Always favor table-driven tests for utility functions with multiple input combinations, rather than ad-hoc individual tests.

## 2024-05-30 - Map Indexing Initialization
**Learning:** Adding new map fields to a struct for O(1) optimization requires not only updating the usages but ensuring those maps are explicitly initialized with `make()` in the struct's instantiation block to avoid runtime panics.
**Action:** When adding map fields, always find the instantiation code (constructor or initialization block) and add the `make(map...)` calls before adding `make` in tests or proceeding with logic changes.
