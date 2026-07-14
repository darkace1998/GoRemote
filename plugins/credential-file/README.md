# Encrypted File

**Status:** Ready

Local credential store encrypted with Argon2id + AES-256-GCM.

## Overview

Package credentialfile implements a built-in credential provider that
stores secrets in a single encrypted file on disk.

The file format is versioned; v1 uses Argon2id + AES-256-GCM with a
user-supplied passphrase. See format.go for the on-disk layout.
