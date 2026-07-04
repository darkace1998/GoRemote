package main
import (
	"fmt"
	"fyne.io/fyne/v2/container"
)
func main() {
	var tabs container.DocTabs
	fmt.Printf("%T\n", tabs.OnReordered)
}
