# 🧪 Add tests for Tree.AddFolder

## Description
🎯 **What:** The `Tree.AddFolder` method in `internal/domain/tree.go` was previously lacking explicit unit tests, creating a gap in testing for domain node invariants (such as rejecting duplicates and invalid configurations).

📊 **Coverage:** This pull request adds the `TestTreeAddFolder` function to `internal/domain/domain_test.go` with exhaustive subtests that now cover:
- Passing a `nil` folder reference.
- Passing a folder with an empty (`NilID`) identifier.
- Attempting to add a folder when its ID is already present as a folder.
- Attempting to add a folder when its ID is already present as a connection.
- Attempting to assign the folder to a `ParentID` that doesn't exist in the tree.
- The happy path involving adding the root folder and successfully parenting a child folder.

✨ **Result:** Test coverage for the `internal/domain` package is significantly improved. The domain model can now be refactored or extended with greater confidence since `Tree.AddFolder` correctness is automatically verified by the test suite. All tests execute successfully with no regressions.
