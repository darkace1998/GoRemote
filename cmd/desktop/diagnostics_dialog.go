package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/darkace1998/GoRemote/app/diagnostics"
	"github.com/darkace1998/GoRemote/app/settings"
	"github.com/darkace1998/GoRemote/app/workspace"
	"github.com/darkace1998/GoRemote/cmd/desktop/tooltip"
)

// showDiagnosticsDialog asks the user where to save a support bundle,
// builds it via app/diagnostics.Build, and reports any non-fatal notes.
func showDiagnosticsDialog(w fyne.Window, b *Bindings) {
	saveDlg := dialog.NewFileSave(func(uc fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if uc == nil {
			return
		}
		go writeDiagnosticsBundle(w, b, uc)
	}, w)
	saveDlg.SetFileName(fmt.Sprintf("goremote-diagnostics-%s.zip", time.Now().UTC().Format("20060102-150405")))
	saveDlg.Show()
}

func writeDiagnosticsBundle(w fyne.Window, b *Bindings, uc fyne.URIWriteCloser) {
	defer uc.Close()
	var settingsPath, workspacePath, pluginRoot string
	if sp, err := settings.DefaultPath(); err == nil {
		settingsPath = sp
	}
	if wp, err := workspace.DefaultPath(); err == nil {
		workspacePath = wp
	}
	if reg := b.PluginRegistry(); reg != nil {
		pluginRoot = reg.Root()
	}
	logs := []string{}
	if lp := b.LogPath(); lp != "" {
		logs = append(logs, lp)
		if _, err := os.Stat(lp + ".1"); err == nil {
			logs = append(logs, lp+".1")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := diagnostics.Build(ctx, uc, diagnostics.Inputs{
		Version:       Version,
		SettingsPath:  settingsPath,
		WorkspacePath: workspacePath,
		LogPaths:      logs,
		LogTailBytes:  2 * 1024 * 1024, // 2 MiB tail per file
		PluginRoot:    pluginRoot,
	})
	fyne.Do(func() {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		msg := fmt.Sprintf("Bundle written: %d bytes.", res.BytesWritten)
		if len(res.Notes) > 0 {
			msg += "\n\nNotes:"
			for _, n := range res.Notes {
				msg += "\n• " + n
			}
		}
		dialog.ShowInformation("Diagnostic bundle", msg, w)
	})
}

// showLogViewerDialog tails the log file and presents it in a scrollable
// read-only view with Refresh and Copy actions. Best-effort: if no log
// file is configured we surface a friendly explanation.
func showLogViewerDialog(w fyne.Window, b *Bindings) {
	lp := b.LogPath()
	if lp == "" {
		dialog.ShowInformation("Log viewer", "No log file is configured. Logs are going to stderr only.", w)
		return
	}

	const tailBytes = 256 * 1024 // 256 KiB

	textArea := widget.NewMultiLineEntry()
	textArea.Wrapping = fyne.TextWrapOff
	textArea.SetMinRowsVisible(20)

	load := func() {
		data, err := readTail(lp, tailBytes)
		if err != nil {
			textArea.SetText(fmt.Sprintf("error reading %s: %v", lp, err))
			return
		}
		textArea.SetText(string(data))
	}
	load()

	refreshBtn := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), load)
	copyBtn := widget.NewButtonWithIcon("Copy all", theme.ContentCopyIcon(), func() {
		fyne.CurrentApp().Clipboard().SetContent(textArea.Text)
	})
	openFolderBtn := widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() {
		_ = openPath(filenameDir(lp))
	})

	pathLabel := widget.NewLabel(lp)
	pathLabel.Wrapping = fyne.TextWrapBreak

	body := container.NewBorder(
		container.NewVBox(pathLabel, container.NewHBox(
			tooltip.WrapButton(refreshBtn, "Reload the tail of the log file"),
			tooltip.WrapButton(copyBtn, "Copy the visible log to the clipboard"),
			tooltip.WrapButton(openFolderBtn, "Open the log folder in your file manager"),
		)),
		nil, nil, nil,
		container.NewStack(textArea),
	)

	dlg := dialog.NewCustom("Log viewer", "Close", body, w)
	dlg.Resize(fyne.NewSize(820, 540))
	dlg.Show()
}

func filenameDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}

func readTail(path string, maxBytes int64) ([]byte, error) {
	// #nosec G304 -- the log path comes from the configured file sink and is read directly for the viewer.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > maxBytes {
		if _, err := f.Seek(st.Size()-maxBytes, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(f)
}

// suppress unused import warnings when building without storage references
var _ = storage.NewFileURI
