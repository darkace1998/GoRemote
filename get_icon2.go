package main

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

func connectionIcon(iconName string, proto string) fyne.Resource {
	if iconName != "" {
		switch iconName {
		case "server":
			return theme.StorageIcon()
		case "database":
			return theme.StorageIcon()
		case "terminal":
			return theme.ComputerIcon()
		case "cloud":
			return theme.UploadIcon()
		case "router":
			return theme.ComputerIcon()
		case "firewall":
			return theme.WarningIcon()
		case "docker":
			return theme.ComputerIcon()
		case "kubernetes":
			return theme.ComputerIcon()
		case "laptop":
			return theme.ComputerIcon()
		case "desktop":
			return theme.ComputerIcon()
		}
	}
	return nil
}

func main() {
    fmt.Println(connectionIcon("server", "ssh").Name())
}
