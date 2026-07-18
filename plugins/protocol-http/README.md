# HTTP/HTTPS

**Status:** Experimental

Fetches HTTP or HTTPS URLs with Go's in-process HTTP client.

## Overview

Package http implements the built-in HTTP / HTTPS protocol plugin for
goremote.

HTTP sessions are handled entirely in-process with Go's net/http package.
No browser or OS launcher is spawned.
