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
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	appsettings "github.com/goremote/goremote/app/settings"
	appworkspace "github.com/goremote/goremote/app/workspace"
	iapp "github.com/goremote/goremote/internal/app"
	"github.com/goremote/goremote/internal/domain"
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
		statusLabel:   statusLabel,
		sessionsLabel: sessionsLabel,
		bindings:      b,
	}
	tabs.OnClosed = func(item *container.TabItem) {
		st := sessions.findByTab(item)
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

	tree := newConnTree(b, func(connID string) {
		openSession(w, b, sessions, connID)
	})
	tree.onError = func(err error) {
		fyne.Do(func() { dialog.ShowError(err, w) })
	}
	sessions.tree = tree

	// Search bar above tree.
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Filter (name, host, tag)…")
	searchEntry.OnChanged = func(text string) {
		tree.setFilter(text)
	}

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

	leftHeader := container.NewBorder(nil, treeActions, nil, nil, searchEntry)
	left := container.NewBorder(leftHeader, nil, nil, nil, tree.tree)

	toolbar := buildToolbar(w, b, tree, sessions, a)

	split := container.NewHSplit(left, tabs)
	split.SetOffset(0.25)

	content := container.NewBorder(toolbar, statusBar, nil, nil, split)
	w.SetContent(content)

	// Confirm-on-close: ask the user when there are open sessions or when
	// the user has explicitly opted into confirmation.
	w.SetCloseIntercept(func() {
		s, _ := b.GetSettings(context.Background())
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

	w.ShowAndRun()
	return true
}

// --- Session registry -----------------------------------------------------

type sessionRegistry struct {
	mu            sync.Mutex
	items         map[domain.ID]*sessionTab
	openConns     map[string]struct{}
	tabs          *container.DocTabs
	statusLabel   *widget.Label
	sessionsLabel *widget.Label
	tree          *connTree
	bindings      *Bindings
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
	r.mu.Unlock()
	r.tabs.Append(st.tabItem)
	r.tabs.Select(st.tabItem)
	r.setStatus(fmt.Sprintf("Connected: %s", st.cv.Name))
	r.setSessionCount(count)
}

func (r *sessionRegistry) remove(hid domain.ID) {
	r.mu.Lock()
	st, ok := r.items[hid]
	if ok {
		delete(r.items, hid)
		delete(r.openConns, st.connID)
	}
	count := len(r.items)
	r.mu.Unlock()
	if ok {
		r.tabs.Remove(st.tabItem)
		r.setStatus(fmt.Sprintf("Disconnected: %s", st.cv.Name))
	}
	r.setSessionCount(count)
}

func (r *sessionRegistry) findByTab(item *container.TabItem) *sessionTab {
	r.mu.Lock()
	defer r.mu.Unlock()
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

	// Drag-and-drop state
	rowsMu     sync.RWMutex
	rows       map[*treeRow]struct{}
	dragSrc    string
	dragTarget string

	// onError is invoked when a drag-and-drop reparent fails.
	onError func(error)
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
	label  *widget.Label
	hilite *canvas.Rectangle
}

func newTreeRow(ct *connTree) *treeRow {
	r := &treeRow{
		ct:     ct,
		icon:   widget.NewIcon(theme.FolderIcon()),
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
	content := container.NewHBox(r.icon, r.label)
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

func (ct *connTree) handleDragMove(srcUID string, abs fyne.Position) {
	ct.dragSrc = srcUID
	drv := fyne.CurrentApp().Driver()
	var target *treeRow
	ct.rowsMu.RLock()
	for row := range ct.rows {
		if row.uid == "" || row.uid == srcUID {
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
		want := color.Color(color.Transparent)
		if row == target {
			want = theme.Color(theme.ColorNameHover)
		}
		if !sameColor(row.hilite.FillColor, want) {
			row.hilite.FillColor = want
			row.hilite.Refresh()
		}
	}
	if target != nil {
		ct.dragTarget = target.uid
	} else {
		ct.dragTarget = ""
	}
}

func (ct *connTree) handleDragEnd() {
	src := ct.dragSrc
	tgt := ct.dragTarget
	ct.dragSrc = ""
	ct.dragTarget = ""

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
		return
	}
	if src == targetParent {
		return // can't drop a node onto itself
	}
	if srcNode.Kind == "folder" && isDescendantOf(srcNode, targetParent) {
		if ct.onError != nil {
			ct.onError(errors.New("cannot move a folder into one of its own descendants"))
		}
		return
	}

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
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.DocumentIcon(), func() { showImportDialog(w, b, tree) }),
		widget.NewToolbarAction(theme.DownloadIcon(), func() { showBackupDialog(w, b) }),
		widget.NewToolbarAction(theme.UploadIcon(), func() { showRestoreDialog(w, b, tree) }),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.SettingsIcon(), func() { showSettingsDialog(w, b, a) }),
		widget.NewToolbarAction(theme.InfoIcon(), func() { showAboutDialog(w) }),
	)
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
			fyne.Do(func() { dialog.ShowError(err, w) })
			return
		}
		attachSession(w, b, sessions, cv, connID, handle)
	}()
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
	hid, err := domain.ParseID(handle)
	if err != nil {
		sessions.releaseConn(connID)
		fyne.Do(func() { dialog.ShowError(err, w) })
		return
	}

	label := cv.Name
	if label == "" {
		label = cv.EffectiveHost
	}
	tabItem := container.NewTabItem(label, widget.NewLabel("Connecting…"))

	st := &sessionTab{
		b:       b,
		cv:      cv,
		handle:  handle,
		hid:     hid,
		connID:  connID,
		tabItem: tabItem,
	}
	st.ctx, st.cancel = context.WithCancel(context.Background())
	tabItem.Content = st.content()

	fyne.Do(func() { sessions.add(st) })

	st.run(func() {
		fyne.Do(func() { sessions.remove(hid) })
	})
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
	d := dialog.NewForm("New Connection", "Create", "Cancel", form.items, func(ok bool) {
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
	d.Resize(fyne.NewSize(540, 480))
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
		CredentialRef: CredentialRefInput{
			ProviderID: cv.CredentialRef.ProviderID,
			Key:        cv.CredentialRef.EntryID,
		},
	}
	form := newConnectionForm(b, in)
	d := dialog.NewForm("Edit Connection", "Save", "Cancel", form.items, func(ok bool) {
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
			Description: ptr(updated.Description),
			Tags:        &updated.Tags,
			Environment: ptr(updated.Environment),
		}
		ref := updated.CredentialRef
		patch.CredentialRef = &ref
		if err := b.UpdateConnection(context.Background(), n.ID, patch); err != nil {
			dialog.ShowError(err, w)
			return
		}
		tree.refresh()
	}, w)
	d.Resize(fyne.NewSize(540, 480))
	d.Show()
}

// connectionForm is the shared edit/new connection form.
type connectionForm struct {
	items []*widget.FormItem

	nameEntry        *widget.Entry
	protoSelect      *widget.Select
	hostEntry        *widget.Entry
	portEntry        *widget.Entry
	userEntry        *widget.Entry
	authSelect       *widget.Select
	descEntry        *widget.Entry
	tagsEntry        *widget.Entry
	envEntry         *widget.Entry
	credProviderEnt  *widget.Entry
	credKeyEntry     *widget.Entry
}

func newConnectionForm(b *Bindings, in ConnectionInput) *connectionForm {
	cf := &connectionForm{}
	cf.nameEntry = widget.NewEntry()
	cf.nameEntry.SetText(in.Name)
	cf.nameEntry.SetPlaceHolder("Connection name")

	cf.protoSelect = widget.NewSelect(availableProtocols(b), nil)
	if in.ProtocolID != "" {
		cf.protoSelect.SetSelected(in.ProtocolID)
	}

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

	authOptions := []string{"", "password", "publickey", "agent", "keyboard-interactive", "none"}
	cf.authSelect = widget.NewSelect(authOptions, nil)
	if in.AuthMethod != "" {
		cf.authSelect.SetSelected(in.AuthMethod)
	}

	cf.descEntry = widget.NewMultiLineEntry()
	cf.descEntry.SetText(in.Description)
	cf.descEntry.Wrapping = fyne.TextWrapWord

	cf.tagsEntry = widget.NewEntry()
	cf.tagsEntry.SetText(strings.Join(in.Tags, ", "))
	cf.tagsEntry.SetPlaceHolder("comma,separated,tags")

	cf.envEntry = widget.NewEntry()
	cf.envEntry.SetText(in.Environment)
	cf.envEntry.SetPlaceHolder("e.g. prod, staging")

	cf.credProviderEnt = widget.NewEntry()
	cf.credProviderEnt.SetText(in.CredentialRef.ProviderID)
	cf.credProviderEnt.SetPlaceHolder("e.g. io.goremote.credential.file")

	cf.credKeyEntry = widget.NewEntry()
	cf.credKeyEntry.SetText(in.CredentialRef.Key)
	cf.credKeyEntry.SetPlaceHolder("entry id in vault (optional)")

	cf.items = []*widget.FormItem{
		widget.NewFormItem("Name", cf.nameEntry),
		widget.NewFormItem("Protocol", cf.protoSelect),
		widget.NewFormItem("Host", cf.hostEntry),
		widget.NewFormItem("Port", cf.portEntry),
		widget.NewFormItem("Username", cf.userEntry),
		widget.NewFormItem("Auth method", cf.authSelect),
		widget.NewFormItem("Description", cf.descEntry),
		widget.NewFormItem("Tags", cf.tagsEntry),
		widget.NewFormItem("Environment", cf.envEntry),
		widget.NewFormItem("Credential provider", cf.credProviderEnt),
		widget.NewFormItem("Credential key", cf.credKeyEntry),
	}
	return cf
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
		CredentialRef: CredentialRefInput{
			ProviderID: strings.TrimSpace(cf.credProviderEnt.Text),
			Key:        strings.TrimSpace(cf.credKeyEntry.Text),
		},
	}, nil
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
	for _, st := range tabs {
		doc.OpenTabs = append(doc.OpenTabs, appworkspace.TabState{
			ID:           st.handle,
			ConnectionID: st.connID,
			Title:        st.cv.Name,
			LastUsedAt:   time.Now(),
		})
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

func restoreWorkspace(w fyne.Window, b *Bindings, sessions *sessionRegistry) {
	if b == nil || b.workspace == nil {
		return
	}
	ws, err := b.GetWorkspace(context.Background())
	if err != nil || len(ws.OpenTabs) == 0 {
		return
	}
	// Hydrate one connection at a time on a goroutine so the UI is responsive.
	go func() {
		for _, t := range ws.OpenTabs {
			if t.ConnectionID == "" {
				continue
			}
			openSession(w, b, sessions, t.ConnectionID)
			time.Sleep(150 * time.Millisecond)
		}
	}()
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
