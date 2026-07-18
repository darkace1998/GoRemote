# 1Password

**Status:** Ready

Resolves credentials from the user's 1Password vault via the 1Password CLI (`op`).

## Overview

Package onepassword implements a credential provider that resolves
secrets from the user's 1Password vault by shelling out to the
official 1Password CLI binary (`op`).

Design notes:
- All `op` invocations are routed through a Runner interface so the
provider is fully testable without a real binary.
- The provider is read-mostly: Resolve / List are implemented, Put
and Delete return credential.ErrReadOnly. Write support could be
added later via `op item create / edit` but is intentionally out
of scope here to keep the trust surface small.
- Unlock pipes the user's master password to `op signin --raw` and
captures the returned session token. Subsequent commands are
invoked with `OP_SESSION_<account>=<token>` in their environment.
