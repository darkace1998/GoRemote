package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	appsettings "github.com/goremote/goremote/app/settings"
	appworkspace "github.com/goremote/goremote/app/workspace"
	iapp "github.com/goremote/goremote/internal/app"
	"github.com/goremote/goremote/internal/domain"
	"github.com/goremote/goremote/internal/logging"
	"github.com/goremote/goremote/sdk/protocol"
)

// Build-time variables.
var (
	Version = "dev"
)

// runGUI creates a Fyne application window, wires up all GUI components,
// applies persisted settings + workspace, and runs the main event loop. It
// returns true so main() knows the GUI owns the loop.
func runGUI(_ *iapp.App, b *Bindings) bool {
	a := fyneapp.NewWithID("com.goremote.desktop")
	applyTheme(a, currentThemePref(b))

	w := a.NewWindow(fmt.Sprintf("goremote %s", Version))
	w.Resize(fyne.NewSize(1200, 750))
	w.CenterOnScreen()

	tabs := container.NewDocTabs()
	tabs.SetTabLocation(container.TabLocationTop)
	tabs.CloseIntercept = nil // we handle close via OnClosed

	statusLabel := widget.NewLabel("Ready")
	sessionsLabel := widget.NewLabel("0 sessions")
	versionLabel := widget.NewLabel(fmt.Sprintf("v%s", Version))
	versionLabel.TextStyle = fyne.TextStyle{Italic: true}
	statusBar := container.NewBorder(nil, nil, statusLabel, container.NewHBox(sessionsLabel, versionLabel))

	sessions := &sessionRegistry{
		tabs:          tabs,
		items:         make(map[domain.ID]*sessionTab),
		openConns:     make(map[string]struct{}),
		groups:        make(map[*container.TabItem]*paneGroup),
		statusLabel:   statusLabel,
		sessionsLabel: sessionsLabel,
		bindings:      b,
	}
	tabs.OnClosed = func(item *container.TabItem) {
		members := sessions.membersOfTab(item)
		if len(members) == 0 {
			// Could be a tab that's currently transferring (single
			// session being detached); fall back to per-tab session
			// lookup so we still suppress in that case.
			st := sessions.findByTab(item)
			if st == nil || st.transferring {
				return
			}
			members = []*sessionTab{st}
		}
		for _, st := range members {
			if st.transferring {
				continue
			}
			st := st
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := st.b.CloseSession(ctx, st.handle); err != nil {
					slog.Warn("close session", "handle", st.handle, "err", err)
				}
			}()
		}
	}

	tree := newConnTree(b, func(connID string) {
		openSession(w, b, sessions, connID)
	})
	tree.onError = func(err error) {
		fyne.Do(func() { dialog.ShowError(err, w) })
	}
	tree.onContextMenu = func(uid string, abs fyne.Position) {
		showTreeContextMenu(w, a, b, tree, sessions, uid, abs)
	}
	sessions.tree = tree

	// Search bar above tree.
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Filter (name, host, tag)…")
	searchEntry.OnChanged = func(text string) {
		tree.setFilter(text)
	}

	envSelect := widget.NewSelect([]string{"All environments"}, nil)
	envSelect.SetSelected("All environments")
	envSelect.OnChanged = func(v string) {
		if v == "All environments" {
			tree.setEnvFilter("")
			return
		}
		tree.setEnvFilter(v)
	}
	// Refresh env choices on demand: each tree.refresh() invocation
	// rebuilds them so newly-tagged connections show up.
	refreshEnvChoices := func() {
		envs := tree.collectEnvironments()
		opts := append([]string{"All environments"}, envs...)
		envSelect.Options = opts
		// Preserve current selection if it still exists.
		cur := envSelect.Selected
		found := false
		for _, o := range opts {
			if o == cur {
				found = true
				break
			}
		}
		if !found {
			envSelect.SetSelected("All environments")
		} else {
			envSelect.Refresh()
		}
	}
	tree.onAfterRefresh = refreshEnvChoices

	// Tree action toolbar.
	treeActions := widget.NewToolbar(
		widget.NewToolbarAction(theme.DocumentCreateIcon(), func() {
			editSelectedNode(w, b, tree)
		}),
		widget.NewToolbarAction(theme.ContentCopyIcon(), func() {
			duplicateSelectedNode(w, b, tree)
		}),
		widget.NewToolbarAction(theme.DeleteIcon(), func() {
			deleteSelectedNode(w, b, tree, sessions)
		}),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.ViewRefreshIcon(), func() {
			tree.refresh()
		}),
	)

	leftHeader := container.NewBorder(envSelect, treeActions, nil, nil, searchEntry)
	left := container.NewBorder(leftHeader, nil, nil, nil, tree.tree)

	toolbar := buildToolbar(w, b, tree, sessions, a)

	split := container.NewHSplit(left, tabs)
	split.SetOffset(0.25)

	content := container.NewBorder(toolbar, statusBar, nil, nil, split)
	w.SetContent(content)

	// Keyboard shortcuts.
	w.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyW, Modifier: fyne.KeyModifierShortcutDefault}, func(_ fyne.Shortcut) {
		closeCurrentSession(sessions)
	})
	w.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyN, Modifier: fyne.KeyModifierShortcutDefault}, func(_ fyne.Shortcut) {
		showNewConnectionDialog(w, b, tree)
	})
	w.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: fyne.KeyModifierShortcutDefault}, func(_ fyne.Shortcut) {
		w.Canvas().Focus(searchEntry)
	})
	w.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyR, Modifier: fyne.KeyModifierShortcutDefault}, func(_ fyne.Shortcut) {
		reconnectCurrentSession(w, b, sessions)
	})
	w.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyD, Modifier: fyne.KeyModifierShortcutDefault}, func(_ fyne.Shortcut) {
		detachCurrentTab(a, sessions)
	})
	// Ctrl+Shift+\ splits right with the currently-selected tree connection;
	// Ctrl+Shift+- splits below. No-op if nothing is selected or no
	// active tab to split into.
	w.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyBackslash,
		Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift,
	}, func(_ fyne.Shortcut) {
		if id := tree.selected(); id != "" {
			openSessionInSplit(w, b, sessions, id, "h")
		}
	})
	w.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyMinus,
		Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift,
	}, func(_ fyne.Shortcut) {
		if id := tree.selected(); id != "" {
			openSessionInSplit(w, b, sessions, id, "v")
		}
	})

	// Confirm-on-close: ask the user when there are open sessions or when
	// the user has explicitly opted into confirmation.
	w.SetCloseIntercept(func() {
		s, err := b.GetSettings(context.Background())
		if err != nil {
			slog.Warn("close-intercept: load settings", "err", err)
		}
		live := sessions.count()
		if !s.ConfirmOnClose && live == 0 {
			persistWorkspace(b, sessions)
			w.Close()
			return
		}
		msg := "Quit goremote?"
		if live > 0 {
			msg = fmt.Sprintf("%d active session(s) will be closed.\n\nQuit goremote?", live)
		}
		dialog.ShowConfirm("Confirm Quit", msg, func(ok bool) {
			if !ok {
				return
			}
			persistWorkspace(b, sessions)
			w.Close()
		}, w)
	})

	tree.refresh()
	restoreWorkspace(w, b, sessions)
	startSessionWatcher(b, sessions)
	installSystemTray(w, a, b, sessions)

	w.ShowAndRun()
	return true
}

// installSystemTray attaches a minimal system-tray icon and menu when
// the runtime supports it (Windows, macOS, most Linux desktops). The
// menu offers Show/Hide, Recent connections, and Quit. On platforms
// without tray support this is a no-op.
func installSystemTray(w fyne.Window, a fyne.App, b *Bindings, sessions *sessionRegistry) {
	deskApp, ok := a.(desktop.App)
	if !ok {
		return
	}
	rebuild := func() *fyne.Menu {
		showItem := fyne.NewMenuItem("Show", func() { fyne.Do(func() { w.Show(); w.RequestFocus() }) })
		quitItem := fyne.NewMenuItem("Quit", func() { fyne.Do(func() { persistWorkspace(b, sessions); a.Quit() }) })
		recents := b.ListRecents(context.Background())
		var recentItem *fyne.MenuItem
		if len(recents) == 0 {
			recentItem = fyne.NewMenuItem("Recent connections (none)", func() {})
			recentItem.Disabled = true
		} else {
			subs := make([]*fyne.MenuItem, 0, len(recents))
			for _, r := range recents {
				r := r
				label := r.Name
				if r.Host != "" {
					label = fmt.Sprintf("%s — %s", r.Name, r.Host)
				}
				subs = append(subs, fyne.NewMenuItem(label, func() {
					fyne.Do(func() {
						w.Show()
						w.RequestFocus()
						openSession(w, b, sessions, r.ID)
					})
				}))
			}
			recentItem = fyne.NewMenuItem("Recent connections", nil)
			recentItem.ChildMenu = fyne.NewMenu("", subs...)
		}
		return fyne.NewMenu("goremote",
			showItem,
			recentItem,
			fyne.NewMenuItemSeparator(),
			quitItem,
		)
	}
	deskApp.SetSystemTrayMenu(rebuild())
	if icon := a.Icon(); icon != nil {
		deskApp.SetSystemTrayIcon(icon)
	}
}

// --- Session registry -----------------------------------------------------

// paneNode is one node in a paneGroup's split tree. A node is either a
// leaf (session != nil, both children nil) or a branch (session nil,
// orientation set, both children non-nil).
type paneNode struct {
	parent *paneNode

	// Leaf state ---------------------------------------------------
	session *sessionTab
	// titleBtn is the small clickable header above a leaf's content
	// that doubles as an active-pane indicator. Lazily built by
	// buildLeaf and reused on subsequent rebuilds.
	titleBtn *widget.Button
	// closeBtn closes just this pane.
	closeBtn *widget.Button

	// Branch state --------------------------------------------------
	orientation string // "h" or "v"
	a, b        *paneNode
}

func (n *paneNode) isLeaf() bool { return n != nil && n.session != nil }

// paneGroup tracks the panes hosted in a single tabItem. Layout is
// represented as a binary tree of paneNodes: each branch is a
// horizontal or vertical split with two children, and each leaf hosts
// exactly one sessionTab. Single-pane tabs have a leaf root; recursive
// splits grow the tree to arbitrary depth.
type paneGroup struct {
	id      string
	tabItem *container.TabItem
	root    *paneNode
	// active points to the currently-focused leaf. Operations that
	// don't carry an explicit target (e.g. tree right-click "Open in
	// split right") apply to this leaf. Always a leaf or nil.
	active *paneNode
}

// leaves returns the session leaves in left-to-right / top-to-bottom
// traversal order. Useful for snapshotting and for label building.
func (g *paneGroup) leaves() []*paneNode {
	var out []*paneNode
	var walk func(n *paneNode)
	walk = func(n *paneNode) {
		if n == nil {
			return
		}
		if n.isLeaf() {
			out = append(out, n)
			return
		}
		walk(n.a)
		walk(n.b)
	}
	walk(g.root)
	return out
}

// leafFor finds the leaf hosting st, or nil.
func (g *paneGroup) leafFor(st *sessionTab) *paneNode {
	for _, lf := range g.leaves() {
		if lf.session == st {
			return lf
		}
	}
	return nil
}

// splitLeaf replaces target (which must be a leaf in this group) with
// a branch whose children are the original target and a new leaf
// hosting newSt. Orientation is "h" or "v". Sets active to the new
// leaf.
func (g *paneGroup) splitLeaf(target *paneNode, newSt *sessionTab, orientation string) *paneNode {
	if target == nil || !target.isLeaf() {
		return nil
	}
	if orientation != "h" && orientation != "v" {
		orientation = "h"
	}
	newLeaf := &paneNode{session: newSt}
	branch := &paneNode{orientation: orientation, a: target, b: newLeaf, parent: target.parent}
	newLeaf.parent = branch
	if target.parent == nil {
		g.root = branch
	} else {
		if target.parent.a == target {
			target.parent.a = branch
		} else {
			target.parent.b = branch
		}
	}
	target.parent = branch
	g.active = newLeaf
	return newLeaf
}

// removeLeaf removes the leaf hosting st. Collapses the surviving
// sibling into the removed leaf's grandparent slot. Returns ok=true
// when the leaf was found, and emptyTab=true when the group is now
// empty (caller should remove the tab).
func (g *paneGroup) removeLeaf(st *sessionTab) (ok, emptyTab bool) {
	leaf := g.leafFor(st)
	if leaf == nil {
		return false, false
	}
	parent := leaf.parent
	if parent == nil {
		// Removing root leaf -> empty group.
		g.root = nil
		g.active = nil
		return true, true
	}
	var sibling *paneNode
	if parent.a == leaf {
		sibling = parent.b
	} else {
		sibling = parent.a
	}
	gp := parent.parent
	sibling.parent = gp
	if gp == nil {
		g.root = sibling
	} else {
		if gp.a == parent {
			gp.a = sibling
		} else {
			gp.b = sibling
		}
	}
	if g.active == leaf {
		// Pick any remaining leaf as the new active; prefer one
		// inside the sibling subtree.
		var pick func(n *paneNode) *paneNode
		pick = func(n *paneNode) *paneNode {
			if n == nil {
				return nil
			}
			if n.isLeaf() {
				return n
			}
			if l := pick(n.a); l != nil {
				return l
			}
			return pick(n.b)
		}
		g.active = pick(sibling)
	}
	return true, false
}

// setActive updates the active leaf and refreshes title button styling.
func (g *paneGroup) setActive(node *paneNode) {
	if node == nil || !node.isLeaf() {
		return
	}
	g.active = node
	for _, lf := range g.leaves() {
		if lf.titleBtn == nil {
			continue
		}
		if lf == node {
			lf.titleBtn.Importance = widget.HighImportance
		} else {
			lf.titleBtn.Importance = widget.MediumImportance
		}
		lf.titleBtn.Refresh()
	}
}

// buildLeaf constructs (or reuses) the canvas chrome for a leaf node.
// The result is a Border with a thin header (title + close button)
// above the session's terminal content widget. Header taps mark the
// leaf as the active pane.
func (g *paneGroup) buildLeaf(node *paneNode) fyne.CanvasObject {
	st := node.session
	if node.titleBtn == nil {
		node.titleBtn = widget.NewButton(tabLabelFor(st), func() {
			g.setActive(node)
		})
		node.titleBtn.Importance = widget.MediumImportance
	} else {
		node.titleBtn.SetText(tabLabelFor(st))
	}
	if node.closeBtn == nil {
		node.closeBtn = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
			handle, b := st.handle, st.b
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := b.CloseSession(ctx, handle); err != nil {
					slog.Warn("close pane", "handle", handle, "err", err)
				}
			}()
		})
		node.closeBtn.Importance = widget.LowImportance
	}
	if g.active == node {
		node.titleBtn.Importance = widget.HighImportance
	} else {
		node.titleBtn.Importance = widget.MediumImportance
	}
	header := container.NewBorder(nil, nil, nil, node.closeBtn, node.titleBtn)
	return container.NewBorder(header, nil, nil, nil, st.content())
}

// build returns the canvas object for an arbitrary subtree.
func (g *paneGroup) build(node *paneNode) fyne.CanvasObject {
	if node == nil {
		return widget.NewLabel("")
	}
	if node.isLeaf() {
		return g.buildLeaf(node)
	}
	left := g.build(node.a)
	right := g.build(node.b)
	var sp *container.Split
	if node.orientation == "v" {
		sp = container.NewVSplit(left, right)
	} else {
		sp = container.NewHSplit(left, right)
	}
	sp.SetOffset(0.5)
	return sp
}

// rebuildLayout replaces the tabItem.Content with the rendered tree.
// Caller must be on the UI goroutine.
func (g *paneGroup) rebuildLayout() {
	if g.tabItem == nil {
		return
	}
	if g.root == nil {
		g.tabItem.Content = widget.NewLabel("")
	} else if g.root.isLeaf() {
		// Single-pane tab: skip the per-leaf chrome to keep the
		// terminal flush with the tab body, matching the pre-Phase-2
		// look.
		g.tabItem.Content = g.root.session.content()
	} else {
		g.tabItem.Content = g.build(g.root)
	}
	g.tabItem.Text = g.label()
	if g.tabItem.Content != nil {
		g.tabItem.Content.Refresh()
	}
}

// label composes the tab title from the leaf names in traversal order.
func (g *paneGroup) label() string {
	leaves := g.leaves()
	switch len(leaves) {
	case 0:
		return ""
	case 1:
		return tabLabelFor(leaves[0].session)
	}
	parts := make([]string, 0, len(leaves))
	for _, lf := range leaves {
		parts = append(parts, tabLabelFor(lf.session))
	}
	return strings.Join(parts, " | ")
}

// memberSessions returns the session pointers for all leaves.
func (g *paneGroup) memberSessions() []*sessionTab {
	leaves := g.leaves()
	out := make([]*sessionTab, 0, len(leaves))
	for _, lf := range leaves {
		out = append(out, lf.session)
	}
	return out
}

func tabLabelFor(st *sessionTab) string {
	if st == nil {
		return ""
	}
	if st.cv.Name != "" {
		return st.cv.Name
	}
	return st.cv.EffectiveHost
}

type sessionRegistry struct {
	mu            sync.Mutex
	items         map[domain.ID]*sessionTab
	openConns     map[string]struct{}
	groups        map[*container.TabItem]*paneGroup
	tabs          *container.DocTabs
	statusLabel   *widget.Label
	sessionsLabel *widget.Label
	tree          *connTree
	bindings      *Bindings
	// addedHook, when non-nil, is invoked at the tail end of every
	// successful add(). Used by restoreWorkspace to await all
	// reopened sessions before applying persisted pane layouts; we
	// keep it as a hook rather than a hard channel so that production
	// code paths don't have to plumb a sentinel chan through every
	// session-open call site.
	addedHook func(*sessionTab)
}

func (r *sessionRegistry) reserveConn(connID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.openConns[connID]; ok {
		return false
	}
	r.openConns[connID] = struct{}{}
	return true
}

func (r *sessionRegistry) releaseConn(connID string) {
	r.mu.Lock()
	delete(r.openConns, connID)
	r.mu.Unlock()
}

func (r *sessionRegistry) add(st *sessionTab) {
	r.mu.Lock()
	r.items[st.hid] = st
	count := len(r.items)
	var g *paneGroup
	var newTab bool
	if st.tabItem != nil {
		g = r.groups[st.tabItem]
		if g == nil {
			g = &paneGroup{tabItem: st.tabItem, id: domain.NewID().String()}
			r.groups[st.tabItem] = g
			newTab = true
		}
		// Skip duplicate-add by checking existing leaves.
		if g.leafFor(st) == nil {
			if g.root == nil {
				leaf := &paneNode{session: st}
				g.root = leaf
				g.active = leaf
			} else {
				target := g.active
				if target == nil || !target.isLeaf() {
					// Fall back to first leaf if active was lost.
					if leaves := g.leaves(); len(leaves) > 0 {
						target = leaves[len(leaves)-1]
					}
				}
				orientation := st.pendingSplit
				if orientation == "" {
					orientation = "h"
				}
				st.pendingSplit = ""
				if target != nil {
					g.splitLeaf(target, st, orientation)
				} else {
					leaf := &paneNode{session: st}
					g.root = leaf
					g.active = leaf
				}
			}
		}
	}
	r.mu.Unlock()
	if st.tabItem != nil && g != nil {
		g.rebuildLayout()
		if newTab {
			r.tabs.Append(st.tabItem)
		}
		r.tabs.Select(st.tabItem)
	}
	r.setStatus(fmt.Sprintf("Connected: %s", st.cv.Name))
	r.setSessionCount(count)
	if hook := r.addedHook; hook != nil {
		hook(st)
	}
}

func (r *sessionRegistry) remove(hid domain.ID) {
	r.mu.Lock()
	st, ok := r.items[hid]
	if ok {
		delete(r.items, hid)
		delete(r.openConns, st.connID)
	}
	count := len(r.items)
	var g *paneGroup
	var removeTab bool
	if ok && st.tabItem != nil {
		g = r.groups[st.tabItem]
		if g != nil {
			_, empty := g.removeLeaf(st)
			if empty {
				delete(r.groups, st.tabItem)
				removeTab = true
			}
		}
	}
	r.mu.Unlock()
	if ok {
		if st.tabItem != nil {
			if removeTab {
				r.tabs.Remove(st.tabItem)
			} else if g != nil {
				g.rebuildLayout()
			}
		}
		if st.window != nil {
			fyne.Do(func() { st.window.Close() })
		}
		r.setStatus(fmt.Sprintf("Disconnected: %s", st.cv.Name))
	}
	r.setSessionCount(count)
}

// groupFor returns the paneGroup hosting the given tabItem, or nil.
func (r *sessionRegistry) groupFor(item *container.TabItem) *paneGroup {
	if item == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.groups[item]
}

// membersOfTab returns a copy of the sessions hosted in the given tab,
// or nil. Safe to call from outside the registry lock.
func (r *sessionRegistry) membersOfTab(item *container.TabItem) []*sessionTab {
	if item == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.groups[item]
	if g == nil {
		return nil
	}
	return g.memberSessions()
}

// findByTab returns the active session within the given tab. For
// multi-pane groups, the active leaf's session is returned; this
// matches "the pane the user is most likely operating on" semantics.
// Returns nil when no group/session is registered.
func (r *sessionRegistry) findByTab(item *container.TabItem) *sessionTab {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g := r.groups[item]; g != nil {
		if g.active != nil && g.active.session != nil {
			return g.active.session
		}
		for _, lf := range g.leaves() {
			return lf.session
		}
	}
	for _, st := range r.items {
		if st.tabItem == item {
			return st
		}
	}
	return nil
}

func (r *sessionRegistry) findByConnection(connID string) *sessionTab {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.items {
		if st.connID == connID {
			return st
		}
	}
	return nil
}

func (r *sessionRegistry) snapshot() []*sessionTab {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*sessionTab, 0, len(r.items))
	for _, st := range r.items {
		out = append(out, st)
	}
	return out
}

func (r *sessionRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}

func (r *sessionRegistry) setStatus(s string) {
	if r.statusLabel == nil {
		return
	}
	fyne.Do(func() { r.statusLabel.SetText(s) })
}

func (r *sessionRegistry) setSessionCount(n int) {
	if r.sessionsLabel == nil {
		return
	}
	label := fmt.Sprintf("%d session", n)
	if n != 1 {
		label += "s"
	}
	fyne.Do(func() { r.sessionsLabel.SetText(label) })
}

// --- Connection tree ------------------------------------------------------

type connTree struct {
	b      *Bindings
	onOpen func(string)
	view   iapp.TreeView
	mu     sync.RWMutex
	tree   *widget.Tree
	selID  string
	filter string
	envSel string

	// Drag-and-drop state
	rowsMu     sync.RWMutex
	rows       map[*treeRow]struct{}
	dragSrc    string
	dragTarget string

	// dragAnim pulses the source row's hilite while a drag is in
	// progress to give the user visual feedback that the drag is live.
	dragAnim    *fyne.Animation
	dragAnimUID string

	// onError is invoked when a drag-and-drop reparent fails.
	onError func(error)

	// onContextMenu is invoked when the user right-clicks a row. The
	// callback receives the node uid and the absolute screen position
	// where the popup menu should appear. Wired from runGUI so the menu
	// can call into the session registry, dialogs, etc., without the
	// tree itself needing to know about them.
	onContextMenu func(uid string, abs fyne.Position)

	// onAfterRefresh is invoked after each successful tree refresh.
	// Used by the env-filter Select to update its options when the
	// tree contents change.
	onAfterRefresh func()
}

func newConnTree(b *Bindings, onOpen func(string)) *connTree {
	ct := &connTree{b: b, onOpen: onOpen, rows: map[*treeRow]struct{}{}}

	ct.tree = widget.NewTree(
		func(uid widget.TreeNodeID) []widget.TreeNodeID { return ct.childUIDs(uid) },
		func(uid widget.TreeNodeID) bool { return ct.isBranch(uid) },
		func(_ bool) fyne.CanvasObject { return ct.createItem() },
		func(uid widget.TreeNodeID, _ bool, obj fyne.CanvasObject) { ct.updateItem(uid, obj) },
	)
	ct.tree.OnSelected = func(uid widget.TreeNodeID) {
		ct.mu.Lock()
		ct.selID = uid
		ct.mu.Unlock()
	}
	ct.tree.OnUnselected = func(_ widget.TreeNodeID) {
		ct.mu.Lock()
		ct.selID = ""
		ct.mu.Unlock()
	}
	ct.tree.OnBranchOpened = func(_ widget.TreeNodeID) {}
	ct.tree.OnBranchClosed = func(_ widget.TreeNodeID) {}
	// Double-click / activate to open a connection.
	// Fyne's tree doesn't expose double-tap; we treat selection on a leaf
	// as the open action when paired with the "Open" toolbar button.

	return ct
}

func (ct *connTree) setFilter(s string) {
	ct.mu.Lock()
	ct.filter = strings.ToLower(strings.TrimSpace(s))
	ct.mu.Unlock()
	ct.tree.Refresh()
}

// setEnvFilter restricts the tree view to connections whose Environment
// matches env (case-insensitive). An empty env disables the filter.
// Folders are kept in the rendered tree if any descendant connection
// passes — otherwise users would lose all context for filtered envs.
func (ct *connTree) setEnvFilter(env string) {
	ct.mu.Lock()
	ct.envSel = strings.ToLower(strings.TrimSpace(env))
	ct.mu.Unlock()
	ct.tree.Refresh()
}

// collectEnvironments walks the tree and returns the sorted set of
// distinct Environment values seen on connections. Used by the toolbar
// env-filter Select to populate its choices.
func (ct *connTree) collectEnvironments() []string {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	if ct.view.Root == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var visit func(n *iapp.NodeView)
	visit = func(n *iapp.NodeView) {
		if n == nil {
			return
		}
		if n.Kind == "connection" && n.Environment != "" {
			seen[n.Environment] = struct{}{}
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(ct.view.Root)
	out := make([]string, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

func (ct *connTree) childUIDs(uid widget.TreeNodeID) []widget.TreeNodeID {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	if ct.view.Root == nil {
		return nil
	}
	var parent *iapp.NodeView
	if uid == "" {
		parent = ct.view.Root
	} else {
		parent = ct.findNode(ct.view.Root, uid)
	}
	if parent == nil {
		return nil
	}
	ids := make([]widget.TreeNodeID, 0, len(parent.Children))
	for _, c := range parent.Children {
		if !ct.matchesFilter(c) {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

func (ct *connTree) matchesFilter(n *iapp.NodeView) bool {
	if !ct.matchesEnv(n) {
		return false
	}
	if ct.filter == "" {
		return true
	}
	if nodeMatches(n, ct.filter) {
		return true
	}
	for _, c := range n.Children {
		if ct.matchesFilter(c) {
			return true
		}
	}
	return false
}

// matchesEnv returns true when n is admitted by the active environment
// filter. Folders are admitted if any descendant connection passes;
// when no filter is set everything is admitted.
func (ct *connTree) matchesEnv(n *iapp.NodeView) bool {
	if ct.envSel == "" {
		return true
	}
	if n.Kind == "connection" {
		return strings.EqualFold(n.Environment, ct.envSel)
	}
	for _, c := range n.Children {
		if ct.matchesEnv(c) {
			return true
		}
	}
	return false
}

func nodeMatches(n *iapp.NodeView, q string) bool {
	if strings.Contains(strings.ToLower(n.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(n.Host), q) {
		return true
	}
	if strings.Contains(strings.ToLower(n.Protocol), q) {
		return true
	}
	if strings.Contains(strings.ToLower(n.Username), q) {
		return true
	}
	if strings.Contains(strings.ToLower(n.Description), q) {
		return true
	}
	for _, t := range n.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

func (ct *connTree) isBranch(uid widget.TreeNodeID) bool {
	if uid == "" {
		return true
	}
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	n := ct.findNode(ct.view.Root, uid)
	return n != nil && n.Kind == "folder"
}

func (ct *connTree) createItem() fyne.CanvasObject {
	return newTreeRow(ct)
}

func (ct *connTree) updateItem(uid widget.TreeNodeID, obj fyne.CanvasObject) {
	ct.mu.RLock()
	n := ct.findNode(ct.view.Root, uid)
	ct.mu.RUnlock()
	row, ok := obj.(*treeRow)
	if !ok {
		return
	}
	row.uid = uid
	if n == nil {
		row.icon.SetResource(theme.QuestionIcon())
		row.label.SetText("")
		return
	}
	if n.Kind == "folder" {
		row.icon.SetResource(theme.FolderIcon())
		row.label.SetText(n.Name)
		row.star.Hide()
	} else {
		row.icon.SetResource(protocolIcon(n.Protocol))
		port := n.Port
		switch {
		case n.Host == "":
			row.label.SetText(n.Name)
		case port == 0:
			row.label.SetText(fmt.Sprintf("%s (%s)", n.Name, n.Host))
		default:
			row.label.SetText(fmt.Sprintf("%s (%s:%d)", n.Name, n.Host, port))
		}
		if n.Favorite {
			row.star.Show()
			row.star.Refresh()
		} else {
			row.star.Hide()
		}
	}
}

// treeRow is the per-row widget rendered inside the connection tree. It owns
// drag-and-drop and supports the "drop a connection on a folder to reparent
// it" UX described in the user-facing docs.
type treeRow struct {
	widget.BaseWidget
	ct     *connTree
	uid    widget.TreeNodeID
	icon   *widget.Icon
	star   *canvas.Text
	label  *widget.Label
	hilite *canvas.Rectangle
}

func newTreeRow(ct *connTree) *treeRow {
	star := canvas.NewText("★", color.RGBA{R: 0xff, G: 0xc1, B: 0x07, A: 0xff})
	star.TextStyle.Bold = true
	star.Hide()
	r := &treeRow{
		ct:     ct,
		icon:   widget.NewIcon(theme.FolderIcon()),
		star:   star,
		label:  widget.NewLabel(""),
		hilite: canvas.NewRectangle(color.Transparent),
	}
	r.ExtendBaseWidget(r)
	ct.rowsMu.Lock()
	ct.rows[r] = struct{}{}
	ct.rowsMu.Unlock()
	return r
}

func (r *treeRow) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewHBox(r.icon, r.label, r.star)
	stack := container.NewStack(r.hilite, content)
	return widget.NewSimpleRenderer(stack)
}

// Dragged is called repeatedly while the user drags this row. We track the
// source uid (this row), then locate the row currently under the cursor by
// scanning all visible row widgets and comparing absolute positions.
func (r *treeRow) Dragged(e *fyne.DragEvent) {
	if r.uid == "" {
		return
	}
	r.ct.handleDragMove(r.uid, e.AbsolutePosition)
}

// DragEnd commits the drop: if a target folder was identified, the dragged
// node is reparented under it; otherwise the operation is a no-op.
func (r *treeRow) DragEnd() {
	r.ct.handleDragEnd()
}

// TappedSecondary opens a per-row context menu (right-click on desktop, or
// long-press / two-finger tap on touch devices). Selection is updated to
// the right-clicked row first so menu actions operate on the obvious
// target instead of whatever was previously selected.
func (r *treeRow) TappedSecondary(e *fyne.PointEvent) {
	if r.uid == "" || r.ct == nil {
		return
	}
	r.ct.tree.Select(r.uid)
	if r.ct.onContextMenu != nil {
		r.ct.onContextMenu(r.uid, e.AbsolutePosition)
	}
}

func (ct *connTree) handleDragMove(srcUID string, abs fyne.Position) {
	ct.dragSrc = srcUID
	drv := fyne.CurrentApp().Driver()
	var target *treeRow
	var srcRow *treeRow
	ct.rowsMu.RLock()
	for row := range ct.rows {
		if row.uid == "" {
			continue
		}
		if row.uid == srcUID {
			srcRow = row
			continue
		}
		pos := drv.AbsolutePositionForObject(row)
		sz := row.Size()
		if sz.Width <= 0 || sz.Height <= 0 {
			continue
		}
		if abs.X >= pos.X && abs.X <= pos.X+sz.Width && abs.Y >= pos.Y && abs.Y <= pos.Y+sz.Height {
			target = row
			break
		}
	}
	rows := make([]*treeRow, 0, len(ct.rows))
	for row := range ct.rows {
		rows = append(rows, row)
	}
	ct.rowsMu.RUnlock()

	for _, row := range rows {
		if row.uid == srcUID {
			// The source row's tint is owned by the drag pulse
			// animation; don't fight it from here.
			continue
		}
		want := color.Color(color.Transparent)
		if row == target {
			want = theme.Color(theme.ColorNameHover)
		}
		if !sameColor(row.hilite.FillColor, want) {
			row.hilite.FillColor = want
			row.hilite.Refresh()
		}
	}

	// (Re)start the source-row pulse animation if we don't already
	// have one running for this uid.
	if srcRow != nil && ct.dragAnimUID != srcUID {
		ct.stopDragAnim()
		ct.startDragAnim(srcRow)
	}

	if target != nil {
		newTgt := target.uid
		if ct.dragTarget != newTgt {
			logging.Trace(ct.b.logger, "tree.dnd target changed",
				slog.String("src", srcUID),
				slog.String("target", newTgt),
			)
		}
		ct.dragTarget = newTgt
	} else {
		if ct.dragTarget != "" {
			logging.Trace(ct.b.logger, "tree.dnd target cleared",
				slog.String("src", srcUID),
			)
		}
		ct.dragTarget = ""
	}
}

// startDragAnim pulses the given row's hilite between the theme's primary
// colour and its hover colour to indicate that this row is being dragged.
func (ct *connTree) startDragAnim(row *treeRow) {
	if row == nil {
		return
	}
	primary := theme.Color(theme.ColorNamePrimary)
	hover := theme.Color(theme.ColorNameHover)
	pr, pg, pb, _ := primary.RGBA()
	start := color.NRGBA{R: uint8(pr >> 8), G: uint8(pg >> 8), B: uint8(pb >> 8), A: 0x55}
	hr, hg, hb, _ := hover.RGBA()
	end := color.NRGBA{R: uint8(hr >> 8), G: uint8(hg >> 8), B: uint8(hb >> 8), A: 0xAA}

	row.hilite.FillColor = start
	row.hilite.Refresh()

	anim := canvas.NewColorRGBAAnimation(start, end, 450*time.Millisecond, func(c color.Color) {
		row.hilite.FillColor = c
		row.hilite.Refresh()
	})
	anim.AutoReverse = true
	anim.RepeatCount = fyne.AnimationRepeatForever
	anim.Start()
	ct.dragAnim = anim
	ct.dragAnimUID = row.uid
}

// stopDragAnim halts the source-row pulse animation, if any, and clears the
// hilite on every row that still references the previous animation uid.
func (ct *connTree) stopDragAnim() {
	if ct.dragAnim != nil {
		ct.dragAnim.Stop()
		ct.dragAnim = nil
	}
	prevUID := ct.dragAnimUID
	ct.dragAnimUID = ""
	if prevUID == "" {
		return
	}
	ct.rowsMu.RLock()
	defer ct.rowsMu.RUnlock()
	for row := range ct.rows {
		if row.uid == prevUID && !sameColor(row.hilite.FillColor, color.Transparent) {
			row.hilite.FillColor = color.Transparent
			row.hilite.Refresh()
		}
	}
}

func (ct *connTree) handleDragEnd() {
	src := ct.dragSrc
	tgt := ct.dragTarget
	ct.dragSrc = ""
	ct.dragTarget = ""

	logging.Trace(ct.b.logger, "tree.dnd drop",
		slog.String("src", src),
		slog.String("target", tgt),
	)

	ct.stopDragAnim()

	ct.rowsMu.RLock()
	rows := make([]*treeRow, 0, len(ct.rows))
	for row := range ct.rows {
		rows = append(rows, row)
	}
	ct.rowsMu.RUnlock()
	for _, row := range rows {
		if !sameColor(row.hilite.FillColor, color.Transparent) {
			row.hilite.FillColor = color.Transparent
			row.hilite.Refresh()
		}
	}

	if src == "" {
		return
	}

	ct.mu.RLock()
	srcNode := ct.findNode(ct.view.Root, src)
	var tgtNode *iapp.NodeView
	if tgt != "" {
		tgtNode = ct.findNode(ct.view.Root, tgt)
	}
	ct.mu.RUnlock()
	if srcNode == nil {
		return
	}

	var targetParent string
	if tgtNode == nil {
		targetParent = "" // root
	} else if tgtNode.Kind == "folder" {
		targetParent = tgtNode.ID
	} else {
		targetParent = tgtNode.ParentID
	}

	if srcNode.ParentID == targetParent {
		logging.Trace(ct.b.logger, "tree.dnd no-op (same parent)",
			slog.String("src", src),
			slog.String("parent", targetParent),
		)
		return
	}
	if src == targetParent {
		logging.Trace(ct.b.logger, "tree.dnd refused (drop on self)",
			slog.String("src", src),
		)
		return // can't drop a node onto itself
	}
	if srcNode.Kind == "folder" && isDescendantOf(srcNode, targetParent) {
		ct.b.logger.Debug("tree.dnd refused (cycle)",
			slog.String("src", src),
			slog.String("target_parent", targetParent),
		)
		if ct.onError != nil {
			ct.onError(errors.New("cannot move a folder into one of its own descendants"))
		}
		return
	}

	ct.b.logger.Debug("tree.dnd commit",
		slog.String("src", src),
		slog.String("src_kind", srcNode.Kind),
		slog.String("src_name", srcNode.Name),
		slog.String("old_parent", srcNode.ParentID),
		slog.String("new_parent", targetParent),
	)
	if err := ct.b.MoveNode(context.Background(), src, targetParent); err != nil {
		if ct.onError != nil {
			ct.onError(err)
		}
		return
	}
	ct.refresh()
}

// isDescendantOf reports whether targetID is in the subtree rooted at n.
func isDescendantOf(n *iapp.NodeView, targetID string) bool {
	if n == nil || targetID == "" {
		return false
	}
	for _, c := range n.Children {
		if c.ID == targetID {
			return true
		}
		if isDescendantOf(c, targetID) {
			return true
		}
	}
	return false
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func (ct *connTree) findNode(root *iapp.NodeView, uid string) *iapp.NodeView {
	if root == nil {
		return nil
	}
	if root.ID == uid {
		return root
	}
	for _, c := range root.Children {
		if found := ct.findNode(c, uid); found != nil {
			return found
		}
	}
	return nil
}

func (ct *connTree) refresh() {
	ctx := context.Background()
	view := ct.b.ListTree(ctx)
	ct.mu.Lock()
	ct.view = view
	ct.mu.Unlock()
	ct.tree.Refresh()
	if ct.onAfterRefresh != nil {
		ct.onAfterRefresh()
	}
}

func (ct *connTree) selected() string {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.selID
}

func (ct *connTree) selectedNode() *iapp.NodeView {
	ct.mu.RLock()
	id := ct.selID
	root := ct.view.Root
	ct.mu.RUnlock()
	if id == "" {
		return nil
	}
	return ct.findNode(root, id)
}

func (ct *connTree) selectedFolder() string {
	n := ct.selectedNode()
	if n == nil {
		return ""
	}
	if n.Kind == "folder" {
		return n.ID
	}
	return n.ParentID
}

// protocolIcon returns a recognizable theme icon per protocol.
func protocolIcon(proto string) fyne.Resource {
	switch strings.ToLower(proto) {
	case "ssh", "telnet", "rlogin", "mosh", "powershell":
		return theme.ComputerIcon()
	case "rdp", "vnc":
		return theme.AccountIcon()
	case "http", "https":
		return theme.HelpIcon()
	case "rawsocket", "tn5250":
		return theme.MailAttachmentIcon()
	default:
		return theme.ComputerIcon()
	}
}

// --- Toolbar --------------------------------------------------------------

func buildToolbar(w fyne.Window, b *Bindings, tree *connTree, sessions *sessionRegistry, a fyne.App) *widget.Toolbar {
	return widget.NewToolbar(
		widget.NewToolbarAction(theme.FolderNewIcon(), func() { showNewFolderDialog(w, b, tree) }),
		widget.NewToolbarAction(theme.ContentAddIcon(), func() { showNewConnectionDialog(w, b, tree) }),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.MediaPlayIcon(), func() {
			id := tree.selected()
			if id == "" {
				dialog.ShowInformation("No selection", "Select a connection to open.", w)
				return
			}
			openSession(w, b, sessions, id)
		}),
		widget.NewToolbarAction(theme.MediaStopIcon(), func() { closeCurrentSession(sessions) }),
		widget.NewToolbarAction(theme.WindowMaximizeIcon(), func() { detachCurrentTab(a, sessions) }),
		widget.NewToolbarAction(theme.HistoryIcon(), func() { showRecentsMenu(w, b, sessions) }),
		widget.NewToolbarAction(theme.SearchIcon(), func() { showFavoritesPicker(w, b, sessions) }),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.DocumentIcon(), func() { showImportDialog(w, b, tree) }),
		widget.NewToolbarAction(theme.DownloadIcon(), func() { showBackupDialog(w, b) }),
		widget.NewToolbarAction(theme.UploadIcon(), func() { showRestoreDialog(w, b, tree) }),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.LoginIcon(), func() { showCredentialsDialog(w, b) }),
		widget.NewToolbarAction(theme.SettingsIcon(), func() { showSettingsDialog(w, b, a) }),
		widget.NewToolbarAction(theme.InfoIcon(), func() { showAboutDialog(w) }),
	)
}

// showRecentsMenu pops a list of recently-opened connections, most-recent
// first, and opens the chosen one.
func showRecentsMenu(w fyne.Window, b *Bindings, sessions *sessionRegistry) {
	items := b.ListRecents(context.Background())
	if len(items) == 0 {
		dialog.ShowInformation("Recents", "No recent connections yet.", w)
		return
	}
	var d *dialog.CustomDialog
	list := widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject {
			return container.NewHBox(widget.NewIcon(theme.ComputerIcon()), widget.NewLabel(""))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container)
			row.Objects[0].(*widget.Icon).SetResource(protocolIcon(items[i].Protocol))
			lbl := row.Objects[1].(*widget.Label)
			n := items[i]
			if n.Host != "" {
				lbl.SetText(fmt.Sprintf("%s — %s", n.Name, n.Host))
			} else {
				lbl.SetText(n.Name)
			}
		},
	)
	list.OnSelected = func(i widget.ListItemID) {
		id := items[i].ID
		if d != nil {
			d.Hide()
		}
		openSession(w, b, sessions, id)
	}
	scroll := container.NewVScroll(list)
	scroll.SetMinSize(fyne.NewSize(360, 320))
	d = dialog.NewCustom("Recent Connections", "Close", scroll, w)
	d.Show()
}

// showFavoritesPicker pops a list of favorited connections and opens the
// chosen one.
func showFavoritesPicker(w fyne.Window, b *Bindings, sessions *sessionRegistry) {
	favs := b.ListFavorites(context.Background())
	if len(favs) == 0 {
		dialog.ShowInformation("Favorites", "No favorite connections yet. Right-click a connection in the tree and choose \"Add to favorites\".", w)
		return
	}
	var d *dialog.CustomDialog
	list := widget.NewList(
		func() int { return len(favs) },
		func() fyne.CanvasObject {
			return container.NewHBox(widget.NewIcon(theme.ComputerIcon()), widget.NewLabel(""))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container)
			row.Objects[0].(*widget.Icon).SetResource(protocolIcon(favs[i].Protocol))
			lbl := row.Objects[1].(*widget.Label)
			n := favs[i]
			if n.Host != "" {
				lbl.SetText(fmt.Sprintf("★ %s — %s", n.Name, n.Host))
			} else {
				lbl.SetText("★ " + n.Name)
			}
		},
	)
	list.OnSelected = func(i widget.ListItemID) {
		id := favs[i].ID
		if d != nil {
			d.Hide()
		}
		openSession(w, b, sessions, id)
	}
	scroll := container.NewVScroll(list)
	scroll.SetMinSize(fyne.NewSize(360, 320))
	d = dialog.NewCustom("Favorite Connections", "Close", scroll, w)
	d.Show()
}

// --- Session management ---------------------------------------------------

func openSession(w fyne.Window, b *Bindings, sessions *sessionRegistry, connID string) {
	if !sessions.reserveConn(connID) {
		// Switch to the existing tab instead of opening a duplicate.
		if st := sessions.findByConnection(connID); st != nil {
			sessions.tabs.Select(st.tabItem)
			return
		}
		dialog.ShowInformation("Already Open", "A session for this connection is already active.", w)
		return
	}

	ctx := context.Background()
	cv, err := b.GetConnection(ctx, connID)
	if err != nil {
		sessions.releaseConn(connID)
		dialog.ShowError(err, w)
		return
	}

	sessions.setStatus(fmt.Sprintf("Connecting to %s…", cv.Name))

	needPassword := needsInteractivePassword(cv)
	if needPassword {
		promptPasswordAndOpen(w, b, sessions, cv, connID)
		return
	}

	go func() {
		handle, err := b.OpenSession(context.Background(), connID)
		if err != nil {
			sessions.releaseConn(connID)
			if isAuthFailure(err) {
				fyne.Do(func() {
					dialog.ShowConfirm("Authentication failed",
						fmt.Sprintf("%v\n\nEnter a password and retry?", err),
						func(retry bool) {
							if !retry {
								return
							}
							if !sessions.reserveConn(connID) {
								return
							}
							promptPasswordAndOpen(w, b, sessions, cv, connID)
						}, w)
				})
				return
			}
			fyne.Do(func() { dialog.ShowError(err, w) })
			return
		}
		attachSession(w, b, sessions, cv, connID, handle)
	}()
}

// isAuthFailure returns true when err looks like an auth/permission
// problem (bad password, wrong key, "permission denied", etc). Used by
// openSession to decide whether to fall back to an interactive
// password prompt instead of a flat error.
func isAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, k := range []string{
		"authenticate", "authentication", "auth fail",
		"permission denied", "unable to authenticate",
		"password", "publickey", "keyboard-interactive",
	} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// needsInteractivePassword reports true when the connection is configured to
// authenticate with a password / keyboard-interactive method but has no
// credential reference, so the GUI must prompt for a password before opening
// the session. SSH connections without any auth method configured also
// trigger this prompt to match typical user expectations.
func needsInteractivePassword(cv iapp.ConnectionView) bool {
	if cv.CredentialRef.ProviderID != "" {
		return false
	}
	switch cv.AuthMethod {
	case "password", "keyboard-interactive":
		return true
	case "":
		return strings.Contains(cv.Protocol, "ssh")
	}
	return false
}

func promptPasswordAndOpen(w fyne.Window, b *Bindings, sessions *sessionRegistry, cv iapp.ConnectionView, connID string) {
	userEntry := widget.NewEntry()
	userEntry.SetText(cv.Username)
	userEntry.SetPlaceHolder("username")
	pwEntry := widget.NewPasswordEntry()
	pwEntry.SetPlaceHolder("password")
	items := []*widget.FormItem{
		widget.NewFormItem("Username", userEntry),
		widget.NewFormItem("Password", pwEntry),
	}
	title := fmt.Sprintf("Sign in to %s", cv.Name)
	d := dialog.NewForm(title, "Connect", "Cancel", items, func(ok bool) {
		if !ok {
			sessions.releaseConn(connID)
			sessions.setStatus("")
			return
		}
		username := strings.TrimSpace(userEntry.Text)
		password := pwEntry.Text
		go func() {
			handle, err := b.OpenSessionWithPassword(context.Background(), connID, username, password)
			if err != nil {
				sessions.releaseConn(connID)
				fyne.Do(func() { dialog.ShowError(err, w) })
				return
			}
			attachSession(w, b, sessions, cv, connID, handle)
		}()
	}, w)
	d.Resize(fyne.NewSize(360, 180))
	d.Show()
}

func attachSession(w fyne.Window, b *Bindings, sessions *sessionRegistry, cv iapp.ConnectionView, connID, handle string) {
	attachSessionInto(w, b, sessions, cv, connID, handle, nil, "")
}

// attachSessionInto attaches a newly opened session into the UI. When
// targetTab is nil a fresh tab is created. When targetTab is non-nil
// the session is wrapped into a split inside that tab using the given
// orientation ("h" or "v"). Splitting an already-split tab is rejected
// at the call site.
func attachSessionInto(w fyne.Window, b *Bindings, sessions *sessionRegistry, cv iapp.ConnectionView, connID, handle string, targetTab *container.TabItem, orientation string) {
	hid, err := domain.ParseID(handle)
	if err != nil {
		// Backend session was opened but we can't track it; close it so
		// it doesn't leak, then release the connection reservation.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = b.CloseSession(ctx, handle)
		}()
		sessions.releaseConn(connID)
		fyne.Do(func() { dialog.ShowError(err, w) })
		return
	}

	label := cv.Name
	if label == "" {
		label = cv.EffectiveHost
	}

	st := &sessionTab{
		b:      b,
		cv:     cv,
		handle: handle,
		hid:    hid,
		connID: connID,
	}
	st.ctx, st.cancel = context.WithCancel(context.Background())

	if targetTab != nil {
		// Validate the target tab still hosts a group; allow any
		// depth — recursive splits target the active leaf inside.
		sessions.mu.Lock()
		g := sessions.groups[targetTab]
		ok := g != nil && g.root != nil
		sessions.mu.Unlock()
		if ok && orientation != "h" && orientation != "v" {
			orientation = "h"
		}
		if !ok {
			// Fall back to opening as its own tab if the target
			// group has been mutated since the user clicked.
			targetTab = nil
		} else {
			st.tabItem = targetTab
			st.pendingSplit = orientation
		}
	}
	if targetTab == nil {
		st.tabItem = container.NewTabItem(label, widget.NewLabel("Connecting…"))
	}

	// Initialize the session's content widget (and st.term for terminal
	// protocols) synchronously here. Otherwise st.run() — which we kick
	// off immediately below — can race against the asynchronous
	// fyne.Do(add) and observe st.term == nil, mistakenly treating an
	// SSH/Telnet session as "external" and never wiring up the
	// terminal. content() is idempotent (cached), so the subsequent
	// rebuildLayout call inside add reuses the same widget.
	st.content()

	fyne.Do(func() { sessions.add(st) })

	st.run(func() {
		fyne.Do(func() { sessions.remove(hid) })
	})
}

// openSessionInSplit opens connID and attaches it as a second pane in
// the currently-selected tab using the given split orientation ("h" or
// "v"). Falls back to a regular tab if no tab is selected or the
// selected tab already hosts two panes.
func openSessionInSplit(w fyne.Window, b *Bindings, sessions *sessionRegistry, connID, orientation string) {
	target := sessions.tabs.Selected()
	if target == nil {
		dialog.ShowInformation("No Active Tab", "Open a session first, then split it.", w)
		return
	}
	if g := sessions.groupFor(target); g == nil || g.root == nil {
		dialog.ShowInformation("Cannot Split", "Selected tab is empty.", w)
		return
	}
	if !sessions.reserveConn(connID) {
		dialog.ShowInformation("Already Open", "A session for this connection is already active.", w)
		return
	}
	ctx := context.Background()
	cv, err := b.GetConnection(ctx, connID)
	if err != nil {
		sessions.releaseConn(connID)
		dialog.ShowError(err, w)
		return
	}
	sessions.setStatus(fmt.Sprintf("Connecting to %s…", cv.Name))
	if needsInteractivePassword(cv) {
		promptPasswordAndAttach(w, b, sessions, cv, connID, target, orientation)
		return
	}
	go func() {
		handle, err := b.OpenSession(context.Background(), connID)
		if err != nil {
			sessions.releaseConn(connID)
			fyne.Do(func() { dialog.ShowError(err, w) })
			return
		}
		attachSessionInto(w, b, sessions, cv, connID, handle, target, orientation)
	}()
}

// promptPasswordAndAttach is the split-aware version of
// promptPasswordAndOpen.
func promptPasswordAndAttach(w fyne.Window, b *Bindings, sessions *sessionRegistry, cv iapp.ConnectionView, connID string, targetTab *container.TabItem, orientation string) {
	userEntry := widget.NewEntry()
	userEntry.SetText(cv.Username)
	userEntry.SetPlaceHolder("username")
	pwEntry := widget.NewPasswordEntry()
	pwEntry.SetPlaceHolder("password")
	items := []*widget.FormItem{
		widget.NewFormItem("Username", userEntry),
		widget.NewFormItem("Password", pwEntry),
	}
	title := fmt.Sprintf("Sign in to %s", cv.Name)
	d := dialog.NewForm(title, "Connect", "Cancel", items, func(ok bool) {
		if !ok {
			sessions.releaseConn(connID)
			sessions.setStatus("")
			return
		}
		username := strings.TrimSpace(userEntry.Text)
		password := pwEntry.Text
		go func() {
			handle, err := b.OpenSessionWithPassword(context.Background(), connID, username, password)
			if err != nil {
				sessions.releaseConn(connID)
				fyne.Do(func() { dialog.ShowError(err, w) })
				return
			}
			attachSessionInto(w, b, sessions, cv, connID, handle, targetTab, orientation)
		}()
	}, w)
	d.Resize(fyne.NewSize(360, 180))
	d.Show()
}

// openSessionInWindow opens a session for connID in a brand-new fyne.Window
// rather than as a tab. Useful when the user wants a side-by-side or
// secondary-monitor layout. Closing the window terminates the session.
func openSessionInWindow(parent fyne.Window, a fyne.App, b *Bindings, sessions *sessionRegistry, connID string) {
	if !sessions.reserveConn(connID) {
		dialog.ShowInformation("Already Open",
			"A session for this connection is already active. Close it before opening a new window.", parent)
		return
	}
	ctx := context.Background()
	cv, err := b.GetConnection(ctx, connID)
	if err != nil {
		sessions.releaseConn(connID)
		dialog.ShowError(err, parent)
		return
	}
	if needsInteractivePassword(cv) {
		promptPasswordAndOpenInWindow(parent, a, b, sessions, cv, connID)
		return
	}
	go func() {
		handle, err := b.OpenSession(context.Background(), connID)
		if err != nil {
			sessions.releaseConn(connID)
			fyne.Do(func() { dialog.ShowError(err, parent) })
			return
		}
		fyne.Do(func() { attachSessionInWindow(a, b, sessions, cv, connID, handle) })
	}()
}

func promptPasswordAndOpenInWindow(parent fyne.Window, a fyne.App, b *Bindings, sessions *sessionRegistry, cv iapp.ConnectionView, connID string) {
	userEntry := widget.NewEntry()
	userEntry.SetText(cv.Username)
	userEntry.SetPlaceHolder("username")
	pwEntry := widget.NewPasswordEntry()
	pwEntry.SetPlaceHolder("password")
	items := []*widget.FormItem{
		widget.NewFormItem("Username", userEntry),
		widget.NewFormItem("Password", pwEntry),
	}
	d := dialog.NewForm(fmt.Sprintf("Sign in to %s", cv.Name), "Connect", "Cancel", items, func(ok bool) {
		if !ok {
			sessions.releaseConn(connID)
			return
		}
		username := strings.TrimSpace(userEntry.Text)
		password := pwEntry.Text
		go func() {
			handle, err := b.OpenSessionWithPassword(context.Background(), connID, username, password)
			if err != nil {
				sessions.releaseConn(connID)
				fyne.Do(func() { dialog.ShowError(err, parent) })
				return
			}
			fyne.Do(func() { attachSessionInWindow(a, b, sessions, cv, connID, handle) })
		}()
	}, parent)
	d.Resize(fyne.NewSize(360, 180))
	d.Show()
}

func attachSessionInWindow(a fyne.App, b *Bindings, sessions *sessionRegistry, cv iapp.ConnectionView, connID, handle string) {
	hid, err := domain.ParseID(handle)
	if err != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = b.CloseSession(ctx, handle)
		}()
		sessions.releaseConn(connID)
		return
	}
	label := cv.Name
	if label == "" {
		label = cv.EffectiveHost
	}
	win := a.NewWindow("goremote — " + label)
	win.Resize(fyne.NewSize(900, 600))

	st := &sessionTab{
		b:      b,
		cv:     cv,
		handle: handle,
		hid:    hid,
		connID: connID,
		window: win,
	}
	st.ctx, st.cancel = context.WithCancel(context.Background())

	reattachBtn := widget.NewButtonWithIcon("Reattach to main", theme.NavigateBackIcon(), func() {
		reattachToMain(sessions, st)
	})
	reattachBar := container.NewHBox(reattachBtn)
	winRoot := container.NewBorder(reattachBar, nil, nil, nil, st.content())
	win.SetContent(winRoot)

	// Window close → terminate the session on a worker (unless we are
	// reattaching the session into the main tab strip, in which case the
	// transfer flag tells us to skip the termination).
	win.SetCloseIntercept(func() {
		if st.transferring {
			win.Close()
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = st.b.CloseSession(ctx, st.handle)
		}()
		win.Close()
	})

	sessions.add(st)
	win.Show()

	go st.run(func() {
		fyne.Do(func() { sessions.remove(hid) })
	})
}

// detachCurrentTab moves the currently-selected tab into a standalone
// floating window without disturbing the running session. The session
// keeps its terminal widget, scrollback, and PTY connection; only the
// parent container changes. No-op when no tab is selected.
func detachCurrentTab(a fyne.App, sessions *sessionRegistry) {
	selected := sessions.tabs.Selected()
	if selected == nil {
		return
	}
	st := sessions.findByTab(selected)
	if st == nil || st.tabItem == nil || st.window != nil {
		return
	}
	// Snapshot what we need before mutating st.
	contentObj := st.content()
	label := st.cv.Name
	if label == "" {
		label = st.cv.EffectiveHost
	}

	st.transferring = true
	// If the source tab hosts another session in a split, peel off
	// just this session and rebuild the tab around the surviving
	// subtree; otherwise remove the whole tab.
	sessions.mu.Lock()
	g := sessions.groups[st.tabItem]
	keepTab := false
	if g != nil {
		_, empty := g.removeLeaf(st)
		if !empty {
			keepTab = true
		} else {
			delete(sessions.groups, st.tabItem)
		}
	}
	sessions.mu.Unlock()
	if keepTab {
		g.rebuildLayout()
	} else {
		sessions.tabs.Remove(st.tabItem)
	}
	st.tabItem = nil

	win := a.NewWindow("goremote — " + label)
	win.Resize(fyne.NewSize(900, 600))

	// Build a small toolbar with a Reattach button so the user can move
	// the session back into the main tab strip later.
	reattachBtn := widget.NewButtonWithIcon("Reattach to main", theme.NavigateBackIcon(), func() {
		reattachToMain(sessions, st)
	})
	reattachBar := container.NewHBox(reattachBtn)
	winRoot := container.NewBorder(reattachBar, nil, nil, nil, contentObj)
	win.SetContent(winRoot)

	win.SetCloseIntercept(func() {
		if st.transferring {
			win.Close()
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = st.b.CloseSession(ctx, st.handle)
		}()
		win.Close()
	})

	st.window = win
	st.transferring = false
	win.Show()
}

// reattachToMain moves a windowed session back into the main tab strip.
// The window is closed without terminating the session; the cached
// content widget is moved to a new TabItem and selected.
func reattachToMain(sessions *sessionRegistry, st *sessionTab) {
	if st == nil || st.window == nil || st.tabItem != nil {
		return
	}
	contentObj := st.content()
	label := st.cv.Name
	if label == "" {
		label = st.cv.EffectiveHost
	}

	st.transferring = true
	st.window.Close()
	st.window = nil

	tabItem := container.NewTabItem(label, contentObj)
	st.tabItem = tabItem
	sessions.mu.Lock()
	leaf := &paneNode{session: st}
	sessions.groups[tabItem] = &paneGroup{tabItem: tabItem, id: domain.NewID().String(), root: leaf, active: leaf}
	sessions.mu.Unlock()
	sessions.tabs.Append(tabItem)
	sessions.tabs.Select(tabItem)
	st.transferring = false
}

func closeCurrentSession(sessions *sessionRegistry) {
	selected := sessions.tabs.Selected()
	if selected == nil {
		return
	}
	st := sessions.findByTab(selected)
	if st == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := st.b.CloseSession(ctx, st.handle); err != nil {
			slog.Warn("close session", "handle", st.handle, "err", err)
		}
	}()
}

// reconnectCurrentSession closes the current tab's session and immediately
// re-opens its connection. Useful when a remote drops or after sleep/wake.
func reconnectCurrentSession(w fyne.Window, b *Bindings, sessions *sessionRegistry) {
	selected := sessions.tabs.Selected()
	if selected == nil {
		return
	}
	st := sessions.findByTab(selected)
	if st == nil {
		return
	}
	connID := st.connID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = st.b.CloseSession(ctx, st.handle)
		// Wait briefly for the run goroutine to remove the tab so
		// reserveConn becomes available.
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if sessions.findByConnection(connID) == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		fyne.Do(func() { openSession(w, b, sessions, connID) })
	}()
}

// startSessionWatcher polls for unexpected session terminations (e.g. server
// hangup) and removes their tabs from the UI. This is a safety net for
// session lifecycles that bypass the normal close path.
func startSessionWatcher(b *Bindings, sessions *sessionRegistry) {
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			active := b.ListSessions()
			alive := make(map[string]struct{}, len(active))
			for _, s := range active {
				alive[s.ID] = struct{}{}
			}
			for _, st := range sessions.snapshot() {
				if _, ok := alive[st.handle]; !ok {
					hid := st.hid
					fyne.Do(func() { sessions.remove(hid) })
				}
			}
		}
	}()
}

// --- Tree node operations -------------------------------------------------

func editSelectedNode(w fyne.Window, b *Bindings, tree *connTree) {
	n := tree.selectedNode()
	if n == nil {
		dialog.ShowInformation("No selection", "Select a folder or connection to edit.", w)
		return
	}
	if n.Kind == "folder" {
		showEditFolderDialog(w, b, tree, n)
	} else {
		showEditConnectionDialog(w, b, tree, n)
	}
}

func deleteSelectedNode(w fyne.Window, b *Bindings, tree *connTree, sessions *sessionRegistry) {
	n := tree.selectedNode()
	if n == nil {
		dialog.ShowInformation("No selection", "Select a folder or connection to delete.", w)
		return
	}
	if n.Kind != "folder" {
		if st := sessions.findByConnection(n.ID); st != nil {
			dialog.ShowInformation("Session active",
				"Disconnect the active session for this connection before deleting it.", w)
			return
		}
	}
	msg := fmt.Sprintf("Delete %q?", n.Name)
	if n.Kind == "folder" {
		msg += "\n\nAll contents will be removed."
	}
	dialog.ShowConfirm("Confirm Delete", msg, func(ok bool) {
		if !ok {
			return
		}
		if err := b.DeleteNode(context.Background(), n.ID); err != nil {
			dialog.ShowError(err, w)
			return
		}
		tree.refresh()
	}, w)
}

func duplicateSelectedNode(w fyne.Window, b *Bindings, tree *connTree) {
	n := tree.selectedNode()
	if n == nil || n.Kind != "connection" {
		dialog.ShowInformation("Duplicate", "Select a connection to duplicate.", w)
		return
	}
	cv, err := b.GetConnection(context.Background(), n.ID)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	in := ConnectionInput{
		Name:        cv.Name + " (copy)",
		ProtocolID:  cv.Protocol,
		Host:        cv.Host,
		Port:        cv.Port,
		Username:    cv.Username,
		AuthMethod:  cv.AuthMethod,
		Description: cv.Description,
		Tags:        append([]string(nil), cv.Tags...),
		Environment: cv.Environment,
		Settings:    cloneSettingsMap(cv.Settings),
		CredentialRef: CredentialRefInput{
			ProviderID: cv.CredentialRef.ProviderID,
			Key:        cv.CredentialRef.EntryID,
		},
	}
	if _, err := b.CreateConnection(context.Background(), n.ParentID, in); err != nil {
		dialog.ShowError(err, w)
		return
	}
	tree.refresh()
}

// --- Dialogs --------------------------------------------------------------

func showNewFolderDialog(w fyne.Window, b *Bindings, tree *connTree) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Folder name")
	descEntry := widget.NewMultiLineEntry()
	descEntry.SetPlaceHolder("Description (optional)")
	descEntry.Wrapping = fyne.TextWrapWord
	tagsEntry := widget.NewEntry()
	tagsEntry.SetPlaceHolder("comma,separated,tags")

	items := []*widget.FormItem{
		widget.NewFormItem("Name", nameEntry),
		widget.NewFormItem("Description", descEntry),
		widget.NewFormItem("Tags", tagsEntry),
	}
	d := dialog.NewForm("New Folder", "Create", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(nameEntry.Text)
		if name == "" {
			dialog.ShowError(errors.New("name is required"), w)
			return
		}
		parent := tree.selectedFolder()
		ctx := context.Background()
		if _, err := b.CreateFolder(ctx, parent, name, descEntry.Text, splitTags(tagsEntry.Text)); err != nil {
			dialog.ShowError(err, w)
			return
		}
		tree.refresh()
	}, w)
	d.Resize(fyne.NewSize(420, 260))
	d.Show()
}

func showEditFolderDialog(w fyne.Window, b *Bindings, tree *connTree, n *iapp.NodeView) {
	nameEntry := widget.NewEntry()
	nameEntry.SetText(n.Name)
	descEntry := widget.NewMultiLineEntry()
	descEntry.SetText(n.Description)
	descEntry.Wrapping = fyne.TextWrapWord
	tagsEntry := widget.NewEntry()
	tagsEntry.SetText(strings.Join(n.Tags, ", "))

	items := []*widget.FormItem{
		widget.NewFormItem("Name", nameEntry),
		widget.NewFormItem("Description", descEntry),
		widget.NewFormItem("Tags", tagsEntry),
	}
	d := dialog.NewForm("Edit Folder", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(nameEntry.Text)
		if name == "" {
			dialog.ShowError(errors.New("name is required"), w)
			return
		}
		desc := descEntry.Text
		tags := splitTags(tagsEntry.Text)
		patch := FolderPatchInput{
			Name:        &name,
			Description: &desc,
			Tags:        &tags,
		}
		if err := b.UpdateFolder(context.Background(), n.ID, patch); err != nil {
			dialog.ShowError(err, w)
			return
		}
		tree.refresh()
	}, w)
	d.Resize(fyne.NewSize(420, 280))
	d.Show()
}

func showNewConnectionDialog(w fyne.Window, b *Bindings, tree *connTree) {
	form := newConnectionForm(b, ConnectionInput{ProtocolID: "ssh"})
	d := dialog.NewCustomConfirm("New Connection", "Create", "Cancel", form.content, func(ok bool) {
		if !ok {
			return
		}
		in, err := form.collect()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		parent := tree.selectedFolder()
		if _, err := b.CreateConnection(context.Background(), parent, in); err != nil {
			dialog.ShowError(err, w)
			return
		}
		tree.refresh()
	}, w)
	d.Resize(fyne.NewSize(560, 600))
	d.Show()
}

func showEditConnectionDialog(w fyne.Window, b *Bindings, tree *connTree, n *iapp.NodeView) {
	cv, err := b.GetConnection(context.Background(), n.ID)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	in := ConnectionInput{
		Name:        cv.Name,
		ProtocolID:  cv.Protocol,
		Host:        cv.Host,
		Port:        cv.Port,
		Username:    cv.Username,
		AuthMethod:  cv.AuthMethod,
		Description: cv.Description,
		Tags:        append([]string(nil), cv.Tags...),
		Environment: cv.Environment,
		Icon:        cv.Icon,
		Color:       cv.Color,
		Favorite:    cv.Favorite,
		Settings:    cloneSettingsMap(cv.Settings),
		CredentialRef: CredentialRefInput{
			ProviderID: cv.CredentialRef.ProviderID,
			Key:        cv.CredentialRef.EntryID,
		},
	}
	form := newConnectionForm(b, in)
	d := dialog.NewCustomConfirm("Edit Connection", "Save", "Cancel", form.content, func(ok bool) {
		if !ok {
			return
		}
		updated, err := form.collect()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		patch := ConnectionPatchInput{
			Name:        ptr(updated.Name),
			ProtocolID:  ptr(updated.ProtocolID),
			Host:        ptr(updated.Host),
			Port:        ptr(updated.Port),
			Username:    ptr(updated.Username),
			AuthMethod:  ptr(updated.AuthMethod),
			Description: ptr(updated.Description),
			Tags:        &updated.Tags,
			Environment: ptr(updated.Environment),
			Icon:        ptr(updated.Icon),
			Color:       ptr(updated.Color),
			Favorite:    ptr(updated.Favorite),
			Settings:    &updated.Settings,
		}
		ref := updated.CredentialRef
		patch.CredentialRef = &ref
		if err := b.UpdateConnection(context.Background(), n.ID, patch); err != nil {
			dialog.ShowError(err, w)
			return
		}
		tree.refresh()
	}, w)
	d.Resize(fyne.NewSize(560, 600))
	d.Show()
}

// settingBinding records a SettingDef plus a getter that returns the current
// value entered in the form (or false if the user left it blank, in which
// case the key is omitted from the saved settings map).
type settingBinding struct {
	def    protocol.SettingDef
	getter func() (any, bool)
}

// connectionForm is the shared edit/new connection form. Top-level fields
// (name, protocol, host, port, username, auth, description, tags,
// environment, credential ref) are static; the "Protocol settings" section
// is rebuilt every time the protocol selector changes so that protocol-
// specific options like RDP's RD Gateway, domain, width/height, fullscreen,
// or VNC's view-only flag are surfaced exactly when they apply.
type connectionForm struct {
	b *Bindings

	nameEntry       *widget.Entry
	protoSelect     *widget.Select
	hostEntry       *widget.Entry
	portEntry       *widget.Entry
	userEntry       *widget.Entry
	authSelect      *widget.Select
	descEntry       *widget.Entry
	tagsEntry       *widget.Entry
	envEntry        *widget.Entry
	colorEntry      *widget.Entry
	iconSelect      *widget.Select
	favoriteCheck   *widget.Check
	credProviderEnt *widget.Entry
	credKeyEntry    *widget.Entry

	protoForm    *widget.Form
	settingBinds []settingBinding

	// initialSettings is the connection's stored settings map; values are
	// used to seed the dynamic widgets when their key matches a SettingDef.
	// As the user edits, we merge widget values back into this map so
	// switching protocols and switching back doesn't silently lose values.
	initialSettings map[string]any

	content fyne.CanvasObject
}

func newConnectionForm(b *Bindings, in ConnectionInput) *connectionForm {
	cf := &connectionForm{
		b:               b,
		initialSettings: cloneSettingsMap(in.Settings),
	}

	cf.nameEntry = widget.NewEntry()
	cf.nameEntry.SetText(in.Name)
	cf.nameEntry.SetPlaceHolder("Connection name")

	cf.hostEntry = widget.NewEntry()
	cf.hostEntry.SetText(in.Host)
	cf.hostEntry.SetPlaceHolder("hostname or IP")

	cf.portEntry = widget.NewEntry()
	if in.Port > 0 {
		cf.portEntry.SetText(strconv.Itoa(in.Port))
	}
	cf.portEntry.SetPlaceHolder("blank = protocol default")

	cf.userEntry = widget.NewEntry()
	cf.userEntry.SetText(in.Username)

	cf.authSelect = widget.NewSelect([]string{""}, nil)

	cf.descEntry = widget.NewMultiLineEntry()
	cf.descEntry.SetText(in.Description)
	cf.descEntry.Wrapping = fyne.TextWrapWord

	cf.tagsEntry = widget.NewEntry()
	cf.tagsEntry.SetText(strings.Join(in.Tags, ", "))
	cf.tagsEntry.SetPlaceHolder("comma,separated,tags")

	cf.envEntry = widget.NewEntry()
	cf.envEntry.SetText(in.Environment)
	cf.envEntry.SetPlaceHolder("e.g. prod, staging")

	cf.colorEntry = widget.NewEntry()
	cf.colorEntry.SetText(in.Color)
	cf.colorEntry.SetPlaceHolder("#RRGGBB or named color (optional)")

	cf.iconSelect = widget.NewSelect(connectionIconChoices(), nil)
	if in.Icon != "" {
		cf.iconSelect.SetSelected(in.Icon)
	} else {
		cf.iconSelect.PlaceHolder = "Default (protocol icon)"
	}

	cf.favoriteCheck = widget.NewCheck("", nil)
	cf.favoriteCheck.SetChecked(in.Favorite)

	cf.credProviderEnt = widget.NewEntry()
	cf.credProviderEnt.SetText(in.CredentialRef.ProviderID)
	cf.credProviderEnt.SetPlaceHolder("e.g. io.goremote.credential.file")

	cf.credKeyEntry = widget.NewEntry()
	cf.credKeyEntry.SetText(in.CredentialRef.Key)
	cf.credKeyEntry.SetPlaceHolder("entry id in vault (optional)")

	cf.protoSelect = widget.NewSelect(availableProtocols(b), func(_ string) {
		cf.snapshotSettings()
		cf.rebuildProtocolSection(in.AuthMethod)
	})

	topForm := widget.NewForm(
		widget.NewFormItem("Name", cf.nameEntry),
		widget.NewFormItem("Protocol", cf.protoSelect),
		widget.NewFormItem("Host", cf.hostEntry),
		widget.NewFormItem("Port", cf.portEntry),
		widget.NewFormItem("Username", cf.userEntry),
		widget.NewFormItem("Auth method", cf.authSelect),
		widget.NewFormItem("Description", cf.descEntry),
		widget.NewFormItem("Tags", cf.tagsEntry),
		widget.NewFormItem("Environment", cf.envEntry),
		widget.NewFormItem("Icon", cf.iconSelect),
		widget.NewFormItem("Color", cf.colorEntry),
		widget.NewFormItem("Favorite", cf.favoriteCheck),
		widget.NewFormItem("Credential provider", cf.credProviderEnt),
		widget.NewFormItem("Credential key", cf.credKeyEntry),
	)

	cf.protoForm = widget.NewForm()
	settingsCard := widget.NewCard("", "Protocol settings", cf.protoForm)
	connCard := widget.NewCard("", "Connection", topForm)

	if in.ProtocolID != "" {
		cf.protoSelect.SetSelected(in.ProtocolID)
	} else {
		cf.rebuildProtocolSection(in.AuthMethod)
	}

	cf.content = container.NewVScroll(container.NewVBox(connCard, settingsCard))
	return cf
}

// snapshotSettings copies the current widget values into initialSettings so
// switching protocols and switching back preserves what the user typed.
func (cf *connectionForm) snapshotSettings() {
	if cf.initialSettings == nil {
		cf.initialSettings = map[string]any{}
	}
	for _, b := range cf.settingBinds {
		if v, ok := b.getter(); ok {
			cf.initialSettings[b.def.Key] = v
		} else {
			delete(cf.initialSettings, b.def.Key)
		}
	}
}

// rebuildProtocolSection rebuilds the auth method dropdown and the
// protocol-specific settings form for the currently selected protocol.
//
// preferAuth is the auth method we'd like to keep selected if the new
// protocol still supports it (used when editing an existing connection so
// the saved auth method is restored on first render).
func (cf *connectionForm) rebuildProtocolSection(preferAuth string) {
	mod, _ := lookupProtocolModule(cf.b, cf.protoSelect.Selected)

	authOpts := []string{""}
	if mod != nil {
		caps := mod.Capabilities()
		for _, am := range caps.AuthMethods {
			authOpts = append(authOpts, string(am))
		}
	} else {
		// Fallback to a generic set when the protocol module is not
		// available (headless mode or unregistered plugin).
		authOpts = []string{"", "password", "publickey", "agent", "keyboard-interactive", "none"}
	}
	cur := cf.authSelect.Selected
	cf.authSelect.Options = authOpts
	want := cur
	if want == "" || !containsString(authOpts, want) {
		want = preferAuth
	}
	if !containsString(authOpts, want) {
		want = ""
	}
	cf.authSelect.SetSelected(want)
	cf.authSelect.Refresh()

	cf.protoForm.Items = nil
	cf.settingBinds = nil
	if mod == nil {
		cf.protoForm.Refresh()
		return
	}
	for _, def := range mod.Settings() {
		// Skip settings already controlled by the top-level fields.
		switch def.Key {
		case "host", "port", "username":
			continue
		}
		cf.addSettingItem(def)
	}
	cf.protoForm.Refresh()
}

func (cf *connectionForm) addSettingItem(def protocol.SettingDef) {
	initial, hasInitial := cf.initialSettings[def.Key]
	binding := settingBinding{def: def}

	var fi *widget.FormItem
	switch def.Type {
	case protocol.SettingBool:
		ck := widget.NewCheck("", nil)
		switch {
		case hasInitial:
			if v, ok := initial.(bool); ok {
				ck.SetChecked(v)
			}
		default:
			if d, ok := def.Default.(bool); ok {
				ck.SetChecked(d)
			}
		}
		binding.getter = func() (any, bool) { return ck.Checked, true }
		fi = widget.NewFormItem(def.Label, ck)

	case protocol.SettingInt:
		e := widget.NewEntry()
		switch {
		case hasInitial:
			e.SetText(stringifyAny(initial))
		case def.Default != nil:
			e.SetText(stringifyAny(def.Default))
		}
		minPtr, maxPtr := def.Min, def.Max
		binding.getter = func() (any, bool) {
			t := strings.TrimSpace(e.Text)
			if t == "" {
				return nil, false
			}
			n, err := strconv.Atoi(t)
			if err != nil {
				return nil, false
			}
			if minPtr != nil && n < *minPtr {
				return nil, false
			}
			if maxPtr != nil && n > *maxPtr {
				return nil, false
			}
			return n, true
		}
		fi = widget.NewFormItem(def.Label, e)

	case protocol.SettingEnum:
		opts := append([]string{""}, def.EnumValues...)
		sel := widget.NewSelect(opts, nil)
		switch {
		case hasInitial:
			if s, ok := initial.(string); ok {
				sel.SetSelected(s)
			}
		default:
			if d, ok := def.Default.(string); ok {
				sel.SetSelected(d)
			}
		}
		binding.getter = func() (any, bool) {
			if sel.Selected == "" {
				return nil, false
			}
			return sel.Selected, true
		}
		fi = widget.NewFormItem(def.Label, sel)

	case protocol.SettingSecret:
		e := widget.NewPasswordEntry()
		if hasInitial {
			e.SetText(stringifyAny(initial))
		}
		binding.getter = func() (any, bool) {
			if e.Text == "" {
				return nil, false
			}
			return e.Text, true
		}
		fi = widget.NewFormItem(def.Label, e)

	default: // SettingString and unknown types fall back to a text entry.
		e := widget.NewEntry()
		switch {
		case hasInitial:
			e.SetText(stringifyAny(initial))
		case def.Default != nil:
			e.SetText(stringifyAny(def.Default))
		}
		binding.getter = func() (any, bool) {
			t := strings.TrimSpace(e.Text)
			if t == "" {
				return nil, false
			}
			return e.Text, true
		}
		fi = widget.NewFormItem(def.Label, e)
	}
	if def.Description != "" {
		fi.HintText = def.Description
	}
	cf.protoForm.AppendItem(fi)
	cf.settingBinds = append(cf.settingBinds, binding)
}

func (cf *connectionForm) collect() (ConnectionInput, error) {
	name := strings.TrimSpace(cf.nameEntry.Text)
	host := strings.TrimSpace(cf.hostEntry.Text)
	if name == "" {
		return ConnectionInput{}, errors.New("name is required")
	}
	if host == "" {
		return ConnectionInput{}, errors.New("host is required")
	}
	if cf.protoSelect.Selected == "" {
		return ConnectionInput{}, errors.New("protocol is required")
	}
	port := 0
	if t := strings.TrimSpace(cf.portEntry.Text); t != "" {
		p, err := strconv.Atoi(t)
		if err != nil || p < 0 || p > 65535 {
			return ConnectionInput{}, errors.New("port must be a number between 0 and 65535")
		}
		port = p
	}

	// Validate protocol settings against their declared min/max bounds and
	// produce nice error messages — bad values are silently dropped by
	// the binding getter, so re-check explicitly here.
	settings := map[string]any{}
	for _, b := range cf.settingBinds {
		if b.def.Type == protocol.SettingInt {
			if t := getEntryText(cf.protoForm, b.def.Label); t != "" {
				n, err := strconv.Atoi(strings.TrimSpace(t))
				if err != nil {
					return ConnectionInput{}, fmt.Errorf("%s must be a whole number", b.def.Label)
				}
				if b.def.Min != nil && n < *b.def.Min {
					return ConnectionInput{}, fmt.Errorf("%s must be ≥ %d", b.def.Label, *b.def.Min)
				}
				if b.def.Max != nil && n > *b.def.Max {
					return ConnectionInput{}, fmt.Errorf("%s must be ≤ %d", b.def.Label, *b.def.Max)
				}
			}
		}
		if v, ok := b.getter(); ok {
			settings[b.def.Key] = v
		}
	}
	if len(settings) == 0 {
		settings = nil
	}

	return ConnectionInput{
		Name:        name,
		ProtocolID:  cf.protoSelect.Selected,
		Host:        host,
		Port:        port,
		Username:    strings.TrimSpace(cf.userEntry.Text),
		AuthMethod:  cf.authSelect.Selected,
		Description: cf.descEntry.Text,
		Tags:        splitTags(cf.tagsEntry.Text),
		Environment: strings.TrimSpace(cf.envEntry.Text),
		Icon:        strings.TrimSpace(cf.iconSelect.Selected),
		Color:       strings.TrimSpace(cf.colorEntry.Text),
		Favorite:    cf.favoriteCheck.Checked,
		Settings:    settings,
		CredentialRef: CredentialRefInput{
			ProviderID: strings.TrimSpace(cf.credProviderEnt.Text),
			Key:        strings.TrimSpace(cf.credKeyEntry.Text),
		},
	}, nil
}

// connectionIconChoices returns the bundled set of icon names the user
// can pick from in the connection editor. The empty string is implied
// (handled by the iconSelect placeholder) and means "use the protocol
// icon".
func connectionIconChoices() []string {
	return []string{
		"server",
		"database",
		"terminal",
		"cloud",
		"router",
		"firewall",
		"docker",
		"kubernetes",
		"laptop",
		"desktop",
	}
}

// getEntryText reads the current text of the entry for the FormItem with the
// given label, or "" if no such item exists. Used by collect() purely for
// re-validation; the canonical value is still produced by the binding
// getter.
func getEntryText(f *widget.Form, label string) string {
	for _, it := range f.Items {
		if it.Text != label {
			continue
		}
		if e, ok := it.Widget.(*widget.Entry); ok {
			return e.Text
		}
	}
	return ""
}

// lookupProtocolModule returns the protocol module registered under either
// the short id ("rdp") or the canonical id ("io.goremote.protocol.rdp").
func lookupProtocolModule(b *Bindings, id string) (protocol.Module, bool) {
	if b == nil || b.app == nil || b.app.ProtocolHost() == nil || id == "" {
		return nil, false
	}
	ph := b.app.ProtocolHost()
	if m, ok := ph.Module(id); ok {
		return m, true
	}
	if !strings.Contains(id, ".") {
		if m, ok := ph.Module("io.goremote.protocol." + strings.ToLower(strings.TrimSpace(id))); ok {
			return m, true
		}
	}
	return nil, false
}

// stringifyAny renders a setting value for display in a text entry. It is
// intentionally tolerant: numbers, bools, strings, and slices all render as
// something the user can edit and re-submit.
func stringifyAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(x)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// cloneSettingsMap returns a shallow copy so the form can mutate
// initialSettings without affecting the original input.
func cloneSettingsMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func containsString(opts []string, v string) bool {
	for _, o := range opts {
		if o == v {
			return true
		}
	}
	return false
}

// availableProtocols returns the protocols actually registered with the host,
// preserving a stable preferred ordering and falling back to the canonical
// list when the host registry is unavailable.
func availableProtocols(b *Bindings) []string {
	preferred := []string{"ssh", "telnet", "rlogin", "rawsocket", "rdp", "vnc", "powershell", "mosh", "tn5250", "http"}
	if b == nil || b.app == nil || b.app.ProtocolHost() == nil {
		return preferred
	}
	mods := b.app.ProtocolHost().List()
	have := make(map[string]bool, len(mods))
	for _, m := range mods {
		manifest := m.Manifest()
		// Strip canonical prefix to keep UI short IDs consistent with
		// connection storage and Open() compatibility.
		short := manifest.ID
		if i := strings.LastIndex(short, "."); i >= 0 {
			short = short[i+1:]
		}
		have[short] = true
	}
	var out []string
	for _, p := range preferred {
		if have[p] {
			out = append(out, p)
		}
	}
	for k := range have {
		if !contains(out, k) {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return preferred
	}
	sort.SliceStable(out, func(i, j int) bool {
		ii, ji := indexOf(preferred, out[i]), indexOf(preferred, out[j])
		if ii < 0 {
			ii = len(preferred)
		}
		if ji < 0 {
			ji = len(preferred)
		}
		if ii != ji {
			return ii < ji
		}
		return out[i] < out[j]
	})
	return out
}

func showImportDialog(w fyne.Window, b *Bindings, tree *connTree) {
	d := dialog.NewFileOpen(func(f fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if f == nil {
			return
		}
		defer f.Close()

		var buf bytes.Buffer
		if _, err := buf.ReadFrom(f); err != nil {
			dialog.ShowError(err, w)
			return
		}
		ctx := context.Background()
		result, err := b.app.ImportMRemoteNG(ctx, "xml", bytes.NewReader(buf.Bytes()))
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		msg := fmt.Sprintf("Imported %d connections, %d folders", result.Imported, result.Folders)
		if len(result.Warnings) > 0 {
			msg += fmt.Sprintf("\n\n%d warning(s):", len(result.Warnings))
			for _, wn := range result.Warnings {
				msg += "\n  • " + wn.Message
			}
		}
		dialog.ShowInformation("Import Complete", msg, w)
		tree.refresh()
	}, w)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".xml", ".csv"}))
	d.Show()
}

func showBackupDialog(w fyne.Window, b *Bindings) {
	dialog.ShowConfirm("Export Snapshot",
		"Export a zip snapshot of the current state to the data directory?",
		func(ok bool) {
			if !ok {
				return
			}
			info, err := b.ExportSnapshot(context.Background())
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			dialog.ShowInformation("Export Complete",
				fmt.Sprintf("Snapshot written to:\n%s", info.Path), w)
		}, w)
}

func showRestoreDialog(w fyne.Window, b *Bindings, tree *connTree) {
	d := dialog.NewFileOpen(func(f fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if f == nil {
			return
		}
		path := f.URI().Path()
		_ = f.Close()
		dialog.ShowConfirm("Restore Snapshot",
			fmt.Sprintf("Replace the current state with the contents of:\n%s ?", path),
			func(ok bool) {
				if !ok {
					return
				}
				if err := b.RestoreSnapshot(context.Background(), path); err != nil {
					dialog.ShowError(err, w)
					return
				}
				tree.refresh()
				dialog.ShowInformation("Restore Complete",
					"Snapshot has been restored. Some changes may require restart.", w)
			}, w)
	}, w)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".zip"}))
	d.Show()
}

func showSettingsDialog(w fyne.Window, b *Bindings, a fyne.App) {
	ctx := context.Background()
	s, err := b.GetSettings(ctx)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}

	themes := []string{appsettings.ThemeSystem, appsettings.ThemeLight, appsettings.ThemeDark}
	themeSelect := widget.NewSelect(themes, nil)
	themeSelect.SetSelected(s.Theme)

	fontFamilyEntry := widget.NewEntry()
	fontFamilyEntry.SetText(s.FontFamily)
	fontFamilyEntry.SetPlaceHolder("blank = system default")

	fontEntry := widget.NewEntry()
	fontEntry.SetText(strconv.Itoa(s.FontSizePx))

	confirmCheck := widget.NewCheck("", nil)
	confirmCheck.SetChecked(s.ConfirmOnClose)

	autoReconnect := widget.NewCheck("", nil)
	autoReconnect.SetChecked(s.AutoReconnect)

	reconMaxEntry := widget.NewEntry()
	reconMaxEntry.SetText(strconv.Itoa(s.ReconnectMaxN))
	reconMaxEntry.SetPlaceHolder(fmt.Sprintf("%d-%d", appsettings.MinReconnectMaxN, appsettings.MaxReconnectMaxN))

	reconDelayEntry := widget.NewEntry()
	reconDelayEntry.SetText(strconv.Itoa(s.ReconnectDelayMs))
	reconDelayEntry.SetPlaceHolder(fmt.Sprintf("%d-%d ms", appsettings.MinReconnectDelayMs, appsettings.MaxReconnectDelayMs))

	telemetryCheck := widget.NewCheck("", nil)
	telemetryCheck.SetChecked(s.TelemetryEnabled)

	logLevels := []string{
		appsettings.LogLevelTrace,
		appsettings.LogLevelDebug,
		appsettings.LogLevelInfo,
		appsettings.LogLevelWarn,
		appsettings.LogLevelError,
	}
	logLevelSelect := widget.NewSelect(logLevels, nil)
	if s.LogLevel == "" {
		s.LogLevel = appsettings.LogLevelInfo
	}
	logLevelSelect.SetSelected(s.LogLevel)

	items := []*widget.FormItem{
		widget.NewFormItem("Theme", themeSelect),
		widget.NewFormItem("Font family", fontFamilyEntry),
		widget.NewFormItem("Font size (px)", fontEntry),
		widget.NewFormItem("Log level", logLevelSelect),
		widget.NewFormItem("Confirm on close", confirmCheck),
		widget.NewFormItem("Auto-reconnect", autoReconnect),
		widget.NewFormItem("Reconnect max attempts", reconMaxEntry),
		widget.NewFormItem("Reconnect delay (ms)", reconDelayEntry),
		widget.NewFormItem("Anonymous telemetry", telemetryCheck),
	}
	d := dialog.NewForm("Settings", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		px, err := strconv.Atoi(strings.TrimSpace(fontEntry.Text))
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid font size: %w", err), w)
			return
		}
		rmax, err := strconv.Atoi(strings.TrimSpace(reconMaxEntry.Text))
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid reconnect max: %w", err), w)
			return
		}
		rdelay, err := strconv.Atoi(strings.TrimSpace(reconDelayEntry.Text))
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid reconnect delay: %w", err), w)
			return
		}
		s.Theme = themeSelect.Selected
		s.FontFamily = strings.TrimSpace(fontFamilyEntry.Text)
		s.FontSizePx = px
		s.LogLevel = logLevelSelect.Selected
		s.ConfirmOnClose = confirmCheck.Checked
		s.AutoReconnect = autoReconnect.Checked
		s.ReconnectMaxN = rmax
		s.ReconnectDelayMs = rdelay
		s.TelemetryEnabled = telemetryCheck.Checked
		updated, err := b.UpdateSettings(ctx, s)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		applyTheme(a, updated.Theme)
		b.SetLogLevel(updated.LogLevel)
		slog.Info("settings updated",
			slog.String("theme", updated.Theme),
			slog.String("logLevel", updated.LogLevel),
			slog.Bool("autoReconnect", updated.AutoReconnect),
			slog.Int("reconnectMaxN", updated.ReconnectMaxN),
			slog.Int("reconnectDelayMs", updated.ReconnectDelayMs),
		)
	}, w)
	d.Resize(fyne.NewSize(480, 460))
	d.Show()
}

func showAboutDialog(w fyne.Window) {
	msg := fmt.Sprintf(
		"goremote %s\n\nA cross-platform connection manager.\nLicensed under the project license.\n\nProtocols: SSH, Telnet, Rlogin, RDP, VNC, RawSocket, PowerShell, Mosh, TN5250, HTTP",
		Version,
	)
	dialog.ShowInformation("About goremote", msg, w)
}

// --- Theme handling --------------------------------------------------------

func applyTheme(a fyne.App, pref string) {
	switch pref {
	case appsettings.ThemeLight:
		a.Settings().SetTheme(theme.LightTheme())
	case appsettings.ThemeDark:
		a.Settings().SetTheme(theme.DarkTheme())
	default:
		a.Settings().SetTheme(theme.DefaultTheme())
	}
}

func currentThemePref(b *Bindings) string {
	s, err := b.GetSettings(context.Background())
	if err != nil {
		return appsettings.ThemeSystem
	}
	if s.Theme == "" {
		return appsettings.ThemeSystem
	}
	return s.Theme
}

// --- Workspace persistence ------------------------------------------------

func persistWorkspace(b *Bindings, sessions *sessionRegistry) {
	if b == nil || b.workspace == nil {
		return
	}
	tabs := sessions.snapshot()
	doc := appworkspace.Default()

	// Build a session->group ID map by snapshotting the registry's
	// groups under the lock so the per-tab loop below can attach the
	// PaneGroup field consistently.
	type groupInfo struct {
		id   string
		size int
	}
	sessions.mu.Lock()
	sessionGroup := make(map[*sessionTab]groupInfo, len(sessions.items))
	layouts := make([]appworkspace.PaneLayout, 0, len(sessions.groups))
	for _, g := range sessions.groups {
		leaves := g.leaves()
		gi := groupInfo{id: g.id, size: len(leaves)}
		for _, lf := range leaves {
			sessionGroup[lf.session] = gi
		}
		if len(leaves) >= 2 {
			layouts = append(layouts, appworkspace.PaneLayout{
				GroupID: g.id,
				Root:    snapshotPaneTree(g.root),
			})
		}
	}
	sessions.mu.Unlock()
	doc.PaneLayouts = layouts

	for _, st := range tabs {
		state := appworkspace.TabState{
			ID:           st.handle,
			ConnectionID: st.connID,
			Title:        st.cv.Name,
			LastUsedAt:   time.Now(),
		}
		if gi, ok := sessionGroup[st]; ok && gi.size > 1 {
			state.PaneGroup = gi.id
		}
		doc.OpenTabs = append(doc.OpenTabs, state)
	}
	if selected := sessions.tabs.Selected(); selected != nil {
		if st := sessions.findByTab(selected); st != nil {
			doc.ActiveTab = st.handle
		}
	}
	doc.UpdatedAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := b.SaveWorkspace(ctx, doc); err != nil {
		slog.Warn("save workspace", "err", err)
	}
}

// snapshotPaneTree converts a runtime paneNode tree into the persisted
// workspace.PaneNode shape (using ConnectionID as the leaf identifier
// so leaves remain matchable across restarts where session handles are
// regenerated).
func snapshotPaneTree(n *paneNode) *appworkspace.PaneNode {
	if n == nil {
		return nil
	}
	if n.isLeaf() {
		return &appworkspace.PaneNode{ConnectionID: n.session.connID}
	}
	return &appworkspace.PaneNode{
		Orientation: n.orientation,
		A:           snapshotPaneTree(n.a),
		B:           snapshotPaneTree(n.b),
	}
}

func restoreWorkspace(w fyne.Window, b *Bindings, sessions *sessionRegistry) {
	if b == nil || b.workspace == nil {
		return
	}
	ws, err := b.GetWorkspace(context.Background())
	if err != nil || len(ws.OpenTabs) == 0 {
		return
	}

	// Pre-collect the connection IDs we are about to reopen so the
	// completion barrier knows what "done" looks like. We listen for
	// matching connID values on the registry's addedHook rather than
	// guessing how long openSession's async pipeline takes — earlier
	// versions used a 150 ms / 500 ms sleep and silently dropped
	// persisted pane layouts whenever a session opened slower than
	// the timer.
	expected := make(map[string]struct{}, len(ws.OpenTabs))
	for _, t := range ws.OpenTabs {
		if t.ConnectionID != "" {
			expected[t.ConnectionID] = struct{}{}
		}
	}
	if len(expected) == 0 {
		return
	}

	done := make(chan struct{}, len(expected))
	prev := sessions.addedHook
	sessions.addedHook = func(st *sessionTab) {
		if prev != nil {
			prev(st)
		}
		if st == nil {
			return
		}
		if _, ok := expected[st.connID]; ok {
			delete(expected, st.connID)
			done <- struct{}{}
		}
	}

	go func() {
		want := len(expected)
		for _, t := range ws.OpenTabs {
			if t.ConnectionID == "" {
				continue
			}
			openSession(w, b, sessions, t.ConnectionID)
		}
		// Wait for every reopened session's add() to land, with a
		// per-session timeout so a session that fails to open never
		// blocks the rest of the restore. Worst case the layout is
		// applied with whichever sessions did make it.
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		got := 0
		for got < want {
			select {
			case <-done:
				got++
			case <-deadline.C:
				slog.Warn("restoreWorkspace: timed out waiting for sessions",
					"want", want, "got", got)
				got = want
			}
		}
		// Drop the hook before queueing the layout pass so subsequent
		// user-driven session opens don't churn through the closure.
		sessions.addedHook = prev
		if len(ws.PaneLayouts) > 0 {
			fyne.Do(func() { restorePaneLayouts(sessions, ws.PaneLayouts) })
		}
	}()
}

// restorePaneLayouts walks the persisted PaneLayouts and merges the
// matching connections' tabs into a single split-pane group per
// layout. Must run on the UI goroutine.
func restorePaneLayouts(sessions *sessionRegistry, layouts []appworkspace.PaneLayout) {
	for _, ly := range layouts {
		if ly.Root == nil {
			continue
		}
		// Resolve all leaf connection IDs to live sessionTabs.
		var connIDs []string
		var collect func(n *appworkspace.PaneNode)
		collect = func(n *appworkspace.PaneNode) {
			if n == nil {
				return
			}
			if n.ConnectionID != "" {
				connIDs = append(connIDs, n.ConnectionID)
				return
			}
			collect(n.A)
			collect(n.B)
		}
		collect(ly.Root)
		if len(connIDs) < 2 {
			continue
		}
		sessionByConn := make(map[string]*sessionTab, len(connIDs))
		sessions.mu.Lock()
		for _, st := range sessions.items {
			sessionByConn[st.connID] = st
		}
		sessions.mu.Unlock()
		members := make([]*sessionTab, 0, len(connIDs))
		for _, cid := range connIDs {
			if st := sessionByConn[cid]; st != nil {
				members = append(members, st)
			}
		}
		if len(members) < 2 {
			continue
		}
		host := members[0]
		hostTab := host.tabItem
		if hostTab == nil {
			continue
		}
		// Detach the others from their current tabs (they're solo
		// tabs at this point), reparent under the host group.
		for _, st := range members[1:] {
			if st.tabItem == nil || st.tabItem == hostTab {
				continue
			}
			sessions.mu.Lock()
			if g := sessions.groups[st.tabItem]; g != nil {
				delete(sessions.groups, st.tabItem)
			}
			sessions.mu.Unlock()
			sessions.tabs.Remove(st.tabItem)
			st.tabItem = hostTab
		}
		// Build the runtime tree from the persisted shape.
		var build func(n *appworkspace.PaneNode) *paneNode
		build = func(n *appworkspace.PaneNode) *paneNode {
			if n == nil {
				return nil
			}
			if n.ConnectionID != "" {
				st := sessionByConn[n.ConnectionID]
				if st == nil {
					return nil
				}
				return &paneNode{session: st}
			}
			a := build(n.A)
			b := build(n.B)
			if a == nil && b == nil {
				return nil
			}
			if a == nil {
				return b
			}
			if b == nil {
				return a
			}
			branch := &paneNode{orientation: n.Orientation, a: a, b: b}
			a.parent = branch
			b.parent = branch
			return branch
		}
		root := build(ly.Root)
		if root == nil {
			continue
		}
		// Pick first leaf as initial active.
		var firstLeaf func(n *paneNode) *paneNode
		firstLeaf = func(n *paneNode) *paneNode {
			if n == nil {
				return nil
			}
			if n.isLeaf() {
				return n
			}
			if l := firstLeaf(n.a); l != nil {
				return l
			}
			return firstLeaf(n.b)
		}
		sessions.mu.Lock()
		g := sessions.groups[hostTab]
		if g == nil {
			g = &paneGroup{tabItem: hostTab, id: ly.GroupID}
			sessions.groups[hostTab] = g
		} else {
			g.id = ly.GroupID
		}
		g.root = root
		g.active = firstLeaf(root)
		sessions.mu.Unlock()
		g.rebuildLayout()
	}
}

// --- helpers ---------------------------------------------------------------

func splitTags(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ptr[T any](v T) *T { return &v }

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func indexOf(xs []string, v string) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

// showCredentialsDialog presents the registered credential providers in a
// list with their current state and a per-provider Unlock / Lock action.
// The Bitwarden CLI provider, for example, shows up as "locked" when bw is
// installed but the master password hasn't been entered yet; clicking
// "Unlock" prompts for the master password and pipes it to `bw unlock`.
func showCredentialsDialog(w fyne.Window, b *Bindings) {
ctx := context.Background()
providers := b.ListCredentialProviders(ctx)

// rebuildContent generates the dialog body. It is captured below by
// individual action callbacks so each successful unlock/lock refreshes
// the listing without dismissing the dialog.
var dlg dialog.Dialog
var rebuild func()
rebuild = func() {
providers = b.ListCredentialProviders(ctx)
rows := []fyne.CanvasObject{}
if len(providers) == 0 {
rows = append(rows, widget.NewLabel("No credential providers registered."))
}
for _, p := range providers {
p := p
row := buildCredentialRow(w, b, p, func() {
if rebuild != nil {
rebuild()
}
})
rows = append(rows, row)
}
body := container.NewVBox(rows...)
if dlg != nil {
dlg.Hide()
}
dlg = dialog.NewCustom("Credential providers", "Close",
container.NewVScroll(body), w)
dlg.Resize(fyne.NewSize(520, 360))
dlg.Show()
}
rebuild()
}

func buildCredentialRow(w fyne.Window, b *Bindings, p CredentialProviderInfo, onChange func()) fyne.CanvasObject {
stateLabel := widget.NewLabel(fmt.Sprintf("%s (%s)", p.Name, p.State))
stateLabel.TextStyle = fyne.TextStyle{Bold: true}

idLabel := widget.NewLabel(p.ID)
idLabel.Wrapping = fyne.TextWrapBreak

var actions []fyne.CanvasObject
switch p.State {
case "locked":
actions = append(actions, widget.NewButtonWithIcon("Unlock", theme.LoginIcon(), func() {
pwEntry := widget.NewPasswordEntry()
pwEntry.SetPlaceHolder("master password")
form := dialog.NewForm("Unlock "+p.Name, "Unlock", "Cancel",
[]*widget.FormItem{widget.NewFormItem("Password", pwEntry)},
func(ok bool) {
if !ok {
return
}
if err := b.UnlockCredentialProvider(context.Background(), p.ID, pwEntry.Text); err != nil {
dialog.ShowError(err, w)
return
}
if onChange != nil {
onChange()
}
}, w)
form.Resize(fyne.NewSize(360, 160))
form.Show()
}))
case "unlocked":
actions = append(actions, widget.NewButtonWithIcon("Lock", theme.LogoutIcon(), func() {
if err := b.LockCredentialProvider(context.Background(), p.ID); err != nil {
dialog.ShowError(err, w)
return
}
if onChange != nil {
onChange()
}
}))
case "unavailable":
hint := widget.NewLabel("Provider unavailable.")
hint.TextStyle = fyne.TextStyle{Italic: true}
actions = append(actions, hint)
}

right := container.NewHBox(actions...)
return container.NewBorder(nil, idLabel, nil, right, stateLabel)
}

// showTreeContextMenu builds and shows a per-row right-click menu for the
// connection tree. Items shown depend on whether the row is a folder or a
// connection. The row is selected before the menu is shown so existing
// selection-driven helpers (editSelectedNode, deleteSelectedNode, etc.)
// operate on it.
func showTreeContextMenu(w fyne.Window, a fyne.App, b *Bindings, tree *connTree, sessions *sessionRegistry, uid string, abs fyne.Position) {
tree.mu.RLock()
root := tree.view.Root
tree.mu.RUnlock()
n := tree.findNode(root, uid)
if n == nil {
return
}
tree.tree.Select(uid)

var items []*fyne.MenuItem
if n.Kind == "connection" {
connID := n.ID
host := n.Host
port := n.Port
connectItem := fyne.NewMenuItem("Connect", func() { openSession(w, b, sessions, connID) })
newWinItem := fyne.NewMenuItem("Open in new window…", func() { openSessionInWindow(w, a, b, sessions, connID) })
splitRightItem := fyne.NewMenuItem("Open in split right", func() { openSessionInSplit(w, b, sessions, connID, "h") })
splitBelowItem := fyne.NewMenuItem("Open in split below", func() { openSessionInSplit(w, b, sessions, connID, "v") })
canSplit := false
if sel := sessions.tabs.Selected(); sel != nil {
if g := sessions.groupFor(sel); g != nil && g.root != nil {
canSplit = true
}
}
splitRightItem.Disabled = !canSplit
splitBelowItem.Disabled = !canSplit
copyHost := fyne.NewMenuItem("Copy host", func() { a.Clipboard().SetContent(host) })
copyHost.Disabled = host == ""
copyHostPort := fyne.NewMenuItem("Copy host:port", func() {
if port > 0 {
a.Clipboard().SetContent(fmt.Sprintf("%s:%d", host, port))
} else {
a.Clipboard().SetContent(host)
}
})
copyHostPort.Disabled = host == ""
favLabel := "Add to favorites"
if n.Favorite {
favLabel = "Remove from favorites"
}
favItem := fyne.NewMenuItem(favLabel, func() {
if _, err := b.ToggleFavorite(context.Background(), connID); err != nil {
dialog.ShowError(err, w)
return
}
tree.refresh()
})
items = append(items,
connectItem,
newWinItem,
splitRightItem,
splitBelowItem,
fyne.NewMenuItemSeparator(),
fyne.NewMenuItem("Edit…", func() { editSelectedNode(w, b, tree) }),
fyne.NewMenuItem("Duplicate", func() { duplicateSelectedNode(w, b, tree) }),
favItem,
fyne.NewMenuItemSeparator(),
copyHost,
copyHostPort,
fyne.NewMenuItemSeparator(),
fyne.NewMenuItem("Delete…", func() { deleteSelectedNode(w, b, tree, sessions) }),
)
} else {
items = append(items,
fyne.NewMenuItem("New connection here…", func() {
showNewConnectionDialog(w, b, tree)
}),
fyne.NewMenuItem("New folder here…", func() {
showNewFolderDialog(w, b, tree)
}),
fyne.NewMenuItemSeparator(),
fyne.NewMenuItem("Expand all", func() {
expandAllUnder(tree, n)
}),
fyne.NewMenuItem("Collapse all", func() {
collapseAllUnder(tree, n)
}),
fyne.NewMenuItemSeparator(),
fyne.NewMenuItem("Edit…", func() { editSelectedNode(w, b, tree) }),
fyne.NewMenuItem("Delete…", func() { deleteSelectedNode(w, b, tree, sessions) }),
)
}
menu := fyne.NewMenu("", items...)
pop := widget.NewPopUpMenu(menu, w.Canvas())
pop.ShowAtPosition(abs)
}

// expandAllUnder expands the given folder and all descendant folders.
func expandAllUnder(tree *connTree, n *iapp.NodeView) {
if n == nil {
return
}
tree.tree.OpenBranch(n.ID)
for _, c := range n.Children {
if c != nil && c.Kind == "folder" {
expandAllUnder(tree, c)
}
}
}

// collapseAllUnder collapses the given folder and all descendant folders.
func collapseAllUnder(tree *connTree, n *iapp.NodeView) {
if n == nil {
return
}
for _, c := range n.Children {
if c != nil && c.Kind == "folder" {
collapseAllUnder(tree, c)
}
}
tree.tree.CloseBranch(n.ID)
}
