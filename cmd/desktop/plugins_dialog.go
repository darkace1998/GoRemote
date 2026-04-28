package main

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/goremote/goremote/app/extplugin"
	"github.com/goremote/goremote/app/marketplace"
	sdkplugin "github.com/goremote/goremote/sdk/plugin"
)

// showPluginsDialog opens the Plugins management UI: discovered plugins,
// trust policy + trusted keys, and the optional marketplace. It is a
// best-effort UI: if no plugin registry is attached (e.g. the on-disk
// state directory was unwritable), it shows a read-only error.
func showPluginsDialog(w fyne.Window, b *Bindings) {
	reg := b.PluginRegistry()
	if reg == nil {
		dialog.ShowError(fmt.Errorf("plugin registry unavailable; check application logs"), w)
		return
	}

	var dlg dialog.Dialog
	var rebuild func()
	rebuild = func() {
		body := buildPluginsBody(w, b, reg, func() {
			if rebuild != nil {
				rebuild()
			}
		})
		if dlg != nil {
			dlg.Hide()
		}
		dlg = dialog.NewCustom("Plugins", "Close", container.NewVScroll(body), w)
		dlg.Resize(fyne.NewSize(720, 540))
		dlg.Show()
	}
	rebuild()
}

func buildPluginsBody(w fyne.Window, b *Bindings, reg *extplugin.Registry, onChange func()) fyne.CanvasObject {
	// --- Section A: Discovered plugins -----------------------------------
	pluginsHeader := widget.NewLabelWithStyle("Discovered plugins", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	refreshBtn := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
		if err := reg.Refresh(); err != nil {
			dialog.ShowError(err, w)
			return
		}
		onChange()
	})
	openFolderBtn := widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() {
		if err := openPath(reg.Root()); err != nil {
			dialog.ShowError(err, w)
		}
	})
	pluginsToolbar := container.NewHBox(refreshBtn, openFolderBtn)

	entries := reg.Entries()
	pluginRows := []fyne.CanvasObject{}
	if len(entries) == 0 {
		pluginRows = append(pluginRows, widget.NewLabel("No plugins discovered. Drop a plugin folder under "+reg.Root()+" and click Refresh."))
	}
	for _, e := range entries {

		pluginRows = append(pluginRows, buildPluginRow(w, reg, e, onChange))
	}

	// --- Section B: Trust policy + keys ----------------------------------
	trustHeader := widget.NewLabelWithStyle("Trust policy", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	policySel := widget.NewSelect([]string{"permissive", "strict"}, func(v string) {
		var p sdkplugin.Policy
		switch v {
		case "strict":
			p = sdkplugin.PolicyStrict
		default:
			p = sdkplugin.PolicyPermissive
		}
		if err := reg.SetPolicy(p); err != nil {
			dialog.ShowError(err, w)
			return
		}
		onChange()
	})
	policySel.SetSelected(string(reg.Policy()))

	keys := reg.TrustedKeys()
	keyRows := []fyne.CanvasObject{}
	if len(keys) == 0 {
		keyRows = append(keyRows, widget.NewLabel("No trusted keys configured."))
	}
	for _, k := range keys {

		fp := k.PubKey
		if len(fp) > 16 {
			fp = fp[:16] + "…"
		}
		row := container.NewBorder(nil, nil, nil,
			widget.NewButtonWithIcon("Remove", theme.DeleteIcon(), func() {
				dialog.ShowConfirm("Remove trusted key", "Remove key "+k.Label+"?", func(ok bool) {
					if !ok {
						return
					}
					if err := reg.RemoveTrustedKey(k.Label); err != nil {
						dialog.ShowError(err, w)
						return
					}
					onChange()
				}, w)
			}),
			widget.NewLabel(k.Label+"  "+fp),
		)
		keyRows = append(keyRows, row)
	}
	addKeyBtn := widget.NewButtonWithIcon("Add trusted key", theme.ContentAddIcon(), func() {
		labelEntry := widget.NewEntry()
		labelEntry.SetPlaceHolder("publisher label")
		keyEntry := widget.NewMultiLineEntry()
		keyEntry.SetPlaceHolder("base64-encoded ed25519 public key (32 bytes)")
		form := dialog.NewForm("Add trusted key", "Add", "Cancel",
			[]*widget.FormItem{
				widget.NewFormItem("Label", labelEntry),
				widget.NewFormItem("Public key", keyEntry),
			},
			func(ok bool) {
				if !ok {
					return
				}
				if err := reg.AddTrustedKey(labelEntry.Text, keyEntry.Text); err != nil {
					dialog.ShowError(err, w)
					return
				}
				onChange()
			}, w)
		form.Resize(fyne.NewSize(480, 240))
		form.Show()
	})

	// --- Section C: Marketplace ------------------------------------------
	marketHeader := widget.NewLabelWithStyle("Marketplace", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("https://example.com/plugins.json")
	currentURL := ""
	if b.settings != nil {
		if s, err := b.settings.Get(context.Background()); err == nil {
			currentURL = s.PluginMarketplaceURL
		}
	}
	urlEntry.SetText(currentURL)

	marketStatus := widget.NewLabel("")
	marketList := container.NewVBox()

	saveURLBtn := widget.NewButtonWithIcon("Save URL", theme.DocumentSaveIcon(), func() {
		if b.settings == nil {
			dialog.ShowError(fmt.Errorf("settings store unavailable"), w)
			return
		}
		ctx := context.Background()
		s, err := b.settings.Get(ctx)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		s.PluginMarketplaceURL = urlEntry.Text
		if _, err := b.settings.Update(ctx, s); err != nil {
			dialog.ShowError(err, w)
			return
		}
		marketStatus.SetText("URL saved.")
	})

	fetchBtn := widget.NewButtonWithIcon("Fetch listings", theme.SearchIcon(), func() {
		raw := urlEntry.Text
		if raw == "" {
			marketStatus.SetText("Enter a marketplace URL.")
			return
		}
		if _, err := url.Parse(raw); err != nil {
			dialog.ShowError(err, w)
			return
		}
		marketStatus.SetText("Fetching…")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			doc, err := marketplace.NewClient().Fetch(ctx, raw)
			fyne.Do(func() {
				if err != nil {
					marketStatus.SetText("Fetch failed: " + err.Error())
					return
				}
				marketStatus.SetText(fmt.Sprintf("%d listing(s).", len(doc.Listings)))
				marketList.Objects = nil
				for _, l := range doc.Listings {

					marketList.Add(buildListingRow(w, reg, l, onChange))
				}
				marketList.Refresh()
			})
		}()
	})

	marketControls := container.NewBorder(nil, nil, nil,
		container.NewHBox(saveURLBtn, fetchBtn), urlEntry)

	return container.NewVBox(
		pluginsHeader, pluginsToolbar,
		container.NewVBox(pluginRows...),
		widget.NewSeparator(),
		trustHeader,
		container.NewBorder(nil, nil, widget.NewLabel("Policy:"), nil, policySel),
		container.NewVBox(keyRows...),
		addKeyBtn,
		widget.NewSeparator(),
		marketHeader,
		marketControls,
		marketStatus,
		marketList,
	)
}

func buildPluginRow(w fyne.Window, reg *extplugin.Registry, e extplugin.Entry, onChange func()) fyne.CanvasObject {
	title := fmt.Sprintf("%s  v%s  [%s]", e.ID, e.Manifest.Version, e.Status)
	if e.Manifest.Name != "" {
		title = fmt.Sprintf("%s — %s  v%s  [%s]", e.Manifest.Name, e.ID, e.Manifest.Version, e.Status)
	}
	header := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	subtitle := fmt.Sprintf("kind=%s  trust=%s", e.Manifest.Kind, e.TrustLevel)
	if e.Error != "" {
		subtitle += "  error=" + e.Error
	}
	sub := widget.NewLabel(subtitle)
	sub.Wrapping = fyne.TextWrapWord

	setStatus := func(s extplugin.EntryStatus) {
		if err := reg.SetStatus(e.ID, s); err != nil {
			dialog.ShowError(err, w)
			return
		}
		onChange()
	}

	enable := widget.NewButtonWithIcon("Enable", theme.ConfirmIcon(), func() { setStatus(extplugin.StatusEnabled) })
	disable := widget.NewButtonWithIcon("Disable", theme.CancelIcon(), func() { setStatus(extplugin.StatusDisabled) })
	quarantine := widget.NewButton("Quarantine", func() { setStatus(extplugin.StatusQuarantined) })
	forget := widget.NewButtonWithIcon("Forget", theme.DeleteIcon(), func() {
		dialog.ShowConfirm("Forget plugin", "Drop "+e.ID+" from the registry? The plugin folder is left on disk.", func(ok bool) {
			if !ok {
				return
			}
			if err := reg.Forget(e.ID); err != nil {
				dialog.ShowError(err, w)
				return
			}
			onChange()
		}, w)
	})

	switch e.Status {
	case extplugin.StatusEnabled:
		enable.Disable()
	case extplugin.StatusDisabled:
		disable.Disable()
	case extplugin.StatusQuarantined:
		quarantine.Disable()
	case extplugin.StatusBroken:
		enable.Disable()
	}

	actions := container.NewHBox(enable, disable, quarantine, forget)
	return container.NewVBox(header, sub, actions, widget.NewSeparator())
}

func buildListingRow(w fyne.Window, reg *extplugin.Registry, l marketplace.Listing, onChange func()) fyne.CanvasObject {
	title := fmt.Sprintf("%s  v%s  (%s)", l.Name, l.Version, l.ID)
	header := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	desc := widget.NewLabel(l.Description)
	desc.Wrapping = fyne.TextWrapWord

	install := widget.NewButtonWithIcon("Install", theme.DownloadIcon(), func() {
		dialog.ShowConfirm("Install plugin",
			fmt.Sprintf("Download %s v%s from %s?", l.ID, l.Version, l.DownloadURL),
			func(ok bool) {
				if !ok {
					return
				}
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					err := marketplace.NewClient().Install(ctx, l, reg.Root())
					fyne.Do(func() {
						if err != nil {
							dialog.ShowError(err, w)
							return
						}
						if err := reg.Refresh(); err != nil {
							dialog.ShowError(err, w)
							return
						}
						dialog.ShowInformation("Installed", l.ID+" installed. Review trust + enable in the plugins list.", w)
						onChange()
					})
				}()
			}, w)
	})

	return container.NewVBox(header, desc, install, widget.NewSeparator())
}

// openPath asks the OS to reveal a directory in the native file manager.
// Best effort: errors are returned but most users only care that *something*
// opened.
func openPath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", p)
	case "windows":
		cmd = exec.Command("explorer", p)
	default:
		cmd = exec.Command("xdg-open", p)
	}
	return cmd.Start()
}
