package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	iapp "github.com/goremote/goremote/internal/app"
	"github.com/goremote/goremote/internal/domain"
)

// runGUI creates a Fyne application window, wires up all GUI components, and
// runs the main event loop. It returns true so main() knows the GUI owns the
// loop.
func runGUI(_ *iapp.App, b *Bindings) bool {
	a := fyneapp.NewWithID("com.goremote.desktop")
	w := a.NewWindow("goremote")
	w.Resize(fyne.NewSize(1200, 750))

	tabs := container.NewAppTabs()
	tabs.SetTabLocation(container.TabLocationTop)

	status := widget.NewLabel("Ready")

	sessions := &sessionRegistry{
		tabs:      tabs,
		items:     make(map[domain.ID]*sessionTab),
		openConns: make(map[string]struct{}),
		status:    status,
	}

	tree := newConnTree(b, func(connID string) {
		openSession(w, b, sessions, connID)
	})

	toolbar := buildToolbar(w, b, tree, sessions)

	left := container.NewBorder(nil, nil, nil, nil, tree.tree)
	split := container.NewHSplit(left, tabs)
	split.SetOffset(0.25)

	content := container.NewBorder(toolbar, status, nil, nil, split)
	w.SetContent(content)

	tree.refresh()

	w.ShowAndRun()
	return true
}

// sessionRegistry owns the live sessionTab map and the AppTabs widget.
type sessionRegistry struct {
	mu        sync.Mutex
	items     map[domain.ID]*sessionTab
	openConns map[string]struct{} // guards one active session per connection ID
	tabs      *container.AppTabs
	status    *widget.Label
}

// reserveConn atomically claims a connection ID for a new session.
// Returns false if a session for that connection is already active.
func (r *sessionRegistry) reserveConn(connID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.openConns[connID]; ok {
		return false
	}
	r.openConns[connID] = struct{}{}
	return true
}

// releaseConn removes the open-connection guard without removing the session.
// Used on error paths before a session tab is ever created.
func (r *sessionRegistry) releaseConn(connID string) {
	r.mu.Lock()
	delete(r.openConns, connID)
	r.mu.Unlock()
}

func (r *sessionRegistry) add(st *sessionTab) {
	r.mu.Lock()
	r.items[st.hid] = st
	r.mu.Unlock()
	r.tabs.Append(st.tabItem)
	r.tabs.Select(st.tabItem)
}

func (r *sessionRegistry) remove(hid domain.ID) {
	r.mu.Lock()
	st, ok := r.items[hid]
	if ok {
		delete(r.items, hid)
		delete(r.openConns, st.connID)
	}
	r.mu.Unlock()
	if ok {
		r.tabs.Remove(st.tabItem)
	}
}

// findByTab returns the session whose tabItem matches the given item.
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

// --- Connection tree -------------------------------------------------------

type connTree struct {
	b      *Bindings
	onOpen func(string)
	view   iapp.TreeView
	mu     sync.RWMutex
	tree   *widget.Tree
	selID  string
}

func newConnTree(b *Bindings, onOpen func(string)) *connTree {
	ct := &connTree{b: b, onOpen: onOpen}

	ct.tree = widget.NewTree(
		func(uid widget.TreeNodeID) []widget.TreeNodeID {
			return ct.childUIDs(uid)
		},
		func(uid widget.TreeNodeID) bool {
			return ct.isBranch(uid)
		},
		func(branch bool) fyne.CanvasObject {
			return ct.createItem(branch)
		},
		func(uid widget.TreeNodeID, branch bool, obj fyne.CanvasObject) {
			ct.updateItem(uid, branch, obj)
		},
	)
	ct.tree.OnSelected = func(uid widget.TreeNodeID) {
		ct.mu.Lock()
		ct.selID = uid
		ct.mu.Unlock()
	}

	return ct
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
		ids = append(ids, c.ID)
	}
	return ids
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

func (ct *connTree) createItem(_ bool) fyne.CanvasObject {
	icon := widget.NewIcon(theme.FolderIcon())
	label := widget.NewLabel("")
	return container.NewHBox(icon, label)
}

func (ct *connTree) updateItem(uid widget.TreeNodeID, _ bool, obj fyne.CanvasObject) {
	ct.mu.RLock()
	n := ct.findNode(ct.view.Root, uid)
	ct.mu.RUnlock()
	if n == nil {
		return
	}
	hbox, ok := obj.(*fyne.Container)
	if !ok || len(hbox.Objects) < 2 {
		return
	}
	icon, ok := hbox.Objects[0].(*widget.Icon)
	if !ok {
		return
	}
	label, ok := hbox.Objects[1].(*widget.Label)
	if !ok {
		return
	}
	if n.Kind == "folder" {
		icon.SetResource(theme.FolderIcon())
		label.SetText(n.Name)
	} else {
		icon.SetResource(theme.ComputerIcon())
		port := n.Port
		if port == 0 {
			label.SetText(fmt.Sprintf("%s (%s)", n.Name, n.Host))
		} else {
			label.SetText(fmt.Sprintf("%s (%s:%d)", n.Name, n.Host, port))
		}
	}
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

func (ct *connTree) selectedFolder() string {
	ct.mu.RLock()
	id := ct.selID
	root := ct.view.Root
	ct.mu.RUnlock()
	if id == "" {
		return ""
	}
	n := ct.findNode(root, id)
	if n == nil {
		return ""
	}
	if n.Kind == "folder" {
		return n.ID
	}
	return n.ParentID
}

// --- Toolbar ---------------------------------------------------------------

func buildToolbar(w fyne.Window, b *Bindings, tree *connTree, sessions *sessionRegistry) *widget.Toolbar {
	return widget.NewToolbar(
		widget.NewToolbarAction(theme.FolderNewIcon(), func() {
			showNewFolderDialog(w, b, tree)
		}),
		widget.NewToolbarAction(theme.ContentAddIcon(), func() {
			showNewConnectionDialog(w, b, tree)
		}),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.MediaPlayIcon(), func() {
			id := tree.selected()
			if id == "" {
				return
			}
			openSession(w, b, sessions, id)
		}),
		widget.NewToolbarAction(theme.MediaStopIcon(), func() {
			closeCurrentSession(sessions)
		}),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.DocumentIcon(), func() {
			showImportDialog(w, b, tree)
		}),
		widget.NewToolbarAction(theme.SettingsIcon(), func() {
			showSettingsDialog(w, b)
		}),
	)
}

// --- Session management ----------------------------------------------------

func openSession(w fyne.Window, b *Bindings, sessions *sessionRegistry, connID string) {
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

	go func() {
		handle, err := b.OpenSession(context.Background(), connID)
		if err != nil {
			sessions.releaseConn(connID)
			dialog.ShowError(err, w)
			return
		}

		hid, err := domain.ParseID(handle)
		if err != nil {
			sessions.releaseConn(connID)
			dialog.ShowError(err, w)
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

		fyne.Do(func() {
			sessions.add(st)
		})

		st.run(func() {
			fyne.Do(func() {
				sessions.remove(hid)
			})
		})
	}()
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
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := st.b.CloseSession(ctx, st.handle); err != nil {
			slog.Warn("close session", "handle", st.handle, "err", err)
		}
	}()
}

// --- Dialogs ---------------------------------------------------------------

func showNewFolderDialog(w fyne.Window, b *Bindings, tree *connTree) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Folder name")
	descEntry := widget.NewEntry()
	descEntry.SetPlaceHolder("Description (optional)")

	items := []*widget.FormItem{
		widget.NewFormItem("Name", nameEntry),
		widget.NewFormItem("Description", descEntry),
	}
	dialog.ShowForm("New Folder", "Create", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		name := nameEntry.Text
		if name == "" {
			return
		}
		parent := tree.selectedFolder()
		ctx := context.Background()
		if _, err := b.CreateFolder(ctx, parent, name, descEntry.Text, nil); err != nil {
			dialog.ShowError(err, w)
			return
		}
		tree.refresh()
	}, w)
}

func showNewConnectionDialog(w fyne.Window, b *Bindings, tree *connTree) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Connection name")

	protocols := []string{"ssh", "telnet", "rlogin", "rawsocket", "rdp", "vnc", "powershell", "mosh"}
	protoSelect := widget.NewSelect(protocols, nil)
	protoSelect.SetSelected("ssh")

	hostEntry := widget.NewEntry()
	hostEntry.SetPlaceHolder("hostname or IP")

	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("0 = protocol default")

	userEntry := widget.NewEntry()

	items := []*widget.FormItem{
		widget.NewFormItem("Name", nameEntry),
		widget.NewFormItem("Protocol", protoSelect),
		widget.NewFormItem("Host", hostEntry),
		widget.NewFormItem("Port", portEntry),
		widget.NewFormItem("Username", userEntry),
	}
	dialog.ShowForm("New Connection", "Create", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		name := nameEntry.Text
		if name == "" || hostEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("name and host are required"), w)
			return
		}
		port := 0
		if portEntry.Text != "" {
			p, err := strconv.Atoi(portEntry.Text)
			if err != nil || p < 0 || p > 65535 {
				dialog.ShowError(fmt.Errorf("port must be a number between 0 and 65535"), w)
				return
			}
			port = p
		}
		parent := tree.selectedFolder()
		ctx := context.Background()
		_, err := b.CreateConnection(ctx, parent, ConnectionInput{
			Name:       name,
			ProtocolID: protoSelect.Selected,
			Host:       hostEntry.Text,
			Port:       port,
			Username:   userEntry.Text,
		})
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		tree.refresh()
	}, w)
}

func showImportDialog(w fyne.Window, b *Bindings, tree *connTree) {
	dialog.ShowFileOpen(func(f fyne.URIReadCloser, err error) {
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
			msg += fmt.Sprintf("\n%d warning(s):", len(result.Warnings))
			for _, wn := range result.Warnings {
				msg += "\n  • " + wn.Message
			}
		}
		dialog.ShowInformation("Import Complete", msg, w)
		tree.refresh()
	}, w)
}

func showSettingsDialog(w fyne.Window, b *Bindings) {
	ctx := context.Background()
	s, err := b.GetSettings(ctx)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}

	themes := []string{"system", "light", "dark"}
	themeSelect := widget.NewSelect(themes, nil)
	themeSelect.SetSelected(s.Theme)

	fontEntry := widget.NewEntry()
	fontEntry.SetText(strconv.Itoa(s.FontSizePx))

	confirmCheck := widget.NewCheck("", nil)
	confirmCheck.SetChecked(s.ConfirmOnClose)

	autoReconnect := widget.NewCheck("", nil)
	autoReconnect.SetChecked(s.AutoReconnect)

	items := []*widget.FormItem{
		widget.NewFormItem("Theme", themeSelect),
		widget.NewFormItem("Font size (px)", fontEntry),
		widget.NewFormItem("Confirm on close", confirmCheck),
		widget.NewFormItem("Auto-reconnect", autoReconnect),
	}
	dialog.ShowForm("Settings", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		px, err := strconv.Atoi(fontEntry.Text)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid font size: %w", err), w)
			return
		}
		s.Theme = themeSelect.Selected
		s.FontSizePx = px
		s.ConfirmOnClose = confirmCheck.Checked
		s.AutoReconnect = autoReconnect.Checked
		if _, err := b.UpdateSettings(ctx, s); err != nil {
			dialog.ShowError(err, w)
		}
	}, w)
}
