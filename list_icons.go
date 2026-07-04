package main
import (
	"fmt"
	"fyne.io/fyne/v2/theme"
	"reflect"
	"strings"
)

func main() {
	t := reflect.TypeOf(theme.DefaultTheme())
	// Print exported methods that return fyne.Resource
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		if m.Type.NumOut() == 1 && m.Type.Out(0).Name() == "Resource" {
			fmt.Println(m.Name)
		}
	}

	// actually theme package has top-level functions like ComputerIcon()
	// Let's use reflect on the theme package or just read the source.
}
