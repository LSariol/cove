package cli

import "fmt"

func plainLog(s string) {
	fmt.Print(s)
}

// Green
func successLog(s string) {
	fmt.Print("\033[32mCove CLI> " + s + "\033[0m")
}

// Yellow
func warningLog(s string) {
	fmt.Println("\033[33mCove CLI> " + s + "\033[0m")
}

// Red
func errorLog(s string) {
	fmt.Println("\033[31mCove CLI> " + s + "\033[0m")
}

// usageLog shows the correct way to call a command.
func usageLog(form string) {
	warningLog("Usage: " + form)
}

// Cyan
func infoLog(s string) {
	fmt.Println("\033[36mCove CLI> " + s + "\033[0m")
}
