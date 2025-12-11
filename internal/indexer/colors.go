package indexer

import "fmt"

const (
	colorReset   = "\033[0m"
	colorBlue    = "\033[34m"
	colorYellow  = "\033[33m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorGreen   = "\033[32m"
	colorRed     = "\033[31m"
	colorMuted   = "\033[37m"
)

func colorize(color, format string, args ...any) string {
	return color + fmt.Sprintf(format, args...) + colorReset
}
