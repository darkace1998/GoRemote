# SFTP

**Status:** Ready

Interactive SFTP file-browser shell over SSH (ls/cd/get/put/mkdir/rm/...).

## Overview

Package sftp implements the built-in SFTP protocol plugin for goremote.

The plugin opens an SSH connection (reusing the SSH plugin's auth /
known-hosts machinery) and runs an interactive SFTP file-browser shell
inside the host terminal. The command set mirrors OpenSSH's `sftp`
CLI — ls, cd, pwd, get, put, mkdir, rmdir, rm, mv, chmod, lcd, lls,
lpwd, help, exit — so users familiar with that tool feel at home.

Rendering as a terminal protocol means the existing fyne-io/terminal
pane infrastructure handles the UI without the host needing a custom
graphical file-browser widget.
