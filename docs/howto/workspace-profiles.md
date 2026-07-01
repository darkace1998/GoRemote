# Workspace Profiles

GoRemote supports multiple independent **Workspace Profiles**. Each profile has its own connection tree, favorites, recent connections list, and open session tabs.

You can use profiles to separate different environments (e.g. "Work" vs "Personal", or "Prod" vs "Lab") so that connections and active tabs are kept isolated.

## Switch profiles

To switch to a different profile:

1. Click the toolbar icon whose tooltip reads `"Workspace Profiles…"`.
2. A dialog will appear showing your current profile.
3. In the **Existing Profile** dropdown, select the profile you want to switch to.
4. Click **Switch**.

GoRemote will immediately load the selected profile's connection tree and any previously open tabs.

## Create a new profile

To create a new, empty profile:

1. Click the toolbar icon whose tooltip reads `"Workspace Profiles…"`.
2. Type a name for your new profile in the **New Profile** field.
3. Click **Switch**.

The new profile will start with an empty connection tree. All new folders and connections you create will be stored in this profile.

## How profiles are stored

Profiles are stored alongside the default workspace in your OS-specific state directory (see [logs-and-diagnostics.md](./logs-and-diagnostics.md)). The `workspace.json` file represents the default profile, and additional profiles are stored as `workspace-<profile-name>.json`.

## Related buttons

| Tooltip | What it does |
|---|---|
| `Workspace Profiles…` | Open the dialog to switch profiles or create a new one. |
