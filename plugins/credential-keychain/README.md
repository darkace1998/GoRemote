# OS Keychain

**Status:** Ready

Stores credentials in the host operating system's native keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service).

## Overview

Package credentialkeychain implements a credential provider backed by
the host operating system's native secret store (macOS Keychain, Windows
Credential Manager, Linux Secret Service). Secrets are stored at rest in
the OS keychain; a non-sensitive index file records which References
exist so that List() does not require enumerating the keychain (an
operation most backends do not support portably).
