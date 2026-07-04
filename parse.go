package main

import (
	"fmt"
	"io/ioutil"
	"strings"
)

func main() {
	b, _ := ioutil.ReadFile("cmd/desktop/gui_fyne.go")
	s := string(b)
	if strings.Contains(s, "namedIcon") {
		fmt.Println("namedIcon exists")
	} else {
		fmt.Println("namedIcon does not exist")
	}
}
