# Managing connections

Once you have created connections and folders (see
[getting-started.md](./getting-started.md)), you can organise, edit,
and delete them. GoRemote supports both single-item and bulk operations
via the tree toolbar.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `managing-connections.png`._

## Single item actions

Select any connection or folder in the tree to enable these actions in
the toolbar:

| Tooltip | What it does |
|---|---|
| `Edit selected connection…` | Open the properties dialog to rename the item or change its settings (protocol, credentials, inheritance). |
| `Duplicate selected` | Create an exact copy of the selected item in the same folder. The new item's name will have `(copy)` appended. |
| `Delete selected` | Remove the selected item. If a folder is deleted, all its contents are also deleted. This action prompts for confirmation. |

## Bulk operations (Multi-select)

To perform actions on multiple items at once, use the multi-select tools.
Unlike traditional desktop trees, GoRemote uses explicit multi-select
checkmarks to build the selection.

1. Select an item in the tree.
2. Click the toolbar icon whose tooltip reads `"Add selection to multi-select"`.
   A checkmark appears next to the item.
3. Repeat for other items.
4. To clear your current selection, click the toolbar icon whose tooltip
   reads `"Clear multi-select"`.

Once you have multiple items selected, you can use the bulk action
buttons:

| Tooltip | What it does |
|---|---|
| `Bulk duplicate selected` | Creates an exact copy of each checked item in the same folder. |
| `Move selected to folder…` | Opens a dialog to pick a new parent folder, then moves all checked items into it. |
| `Bulk delete selected` | Deletes all checked items after a single confirmation prompt. Any active sessions for deleted connections are forcefully closed. |

*Note: Multi-row bulk editing of connection properties is not yet
supported.*

## Favorites

You can mark frequently used connections as favorites to access them quickly from the `"Open a favorite…"` toolbar picker. A yellow ★ icon appears next to favorited connections in the tree.

To add or remove a favorite:
* **Context menu:** Right-click a connection in the tree and choose **"Add to favorites"** or **"Remove from favorites"**.
* **Properties dialog:** Edit the connection and toggle the **Favorite** checkbox.

## Drag and drop

GoRemote supports drag-and-drop to reorder items or move them between
folders. Drag an item and drop it onto a folder to move it inside, or
drop it between two items to place it there.

## Related buttons

| Tooltip | What it does |
|---|---|
| `Edit selected connection…` | Change connection properties |
| `Duplicate selected` | Clone a connection or folder |
| `Delete selected` | Remove a single item |
| `Add selection to multi-select` | Mark an item for bulk action |
| `Clear multi-select` | Deselect all marked items |
| `Bulk duplicate selected` | Bulk clone marked items |
| `Move selected to folder…` | Bulk move marked items |
| `Bulk delete selected` | Bulk delete marked items |
