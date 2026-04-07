package ui

import (
	"fmt"

	"github.com/fatih/color"
)

var (
	dim     = color.New(color.FgHiBlack)
	info    = color.New(color.FgCyan)
	success = color.New(color.FgGreen)
	warn    = color.New(color.FgYellow)
	err_    = color.New(color.FgRed)

	bold      = color.New(color.Bold)
	boldGreen = color.New(color.Bold, color.FgGreen)
	boldRed   = color.New(color.Bold, color.FgRed)
)

// Step prints a labeled progress step (e.g. "Connecting to DSQL...").
func Step(msg string, args ...interface{}) {
	dim.Print("  → ")
	fmt.Printf(msg+"\n", args...)
}

// Info prints an informational message in cyan.
func Info(msg string, args ...interface{}) {
	info.Printf(msg+"\n", args...)
}

// Success prints a success message in green.
func Success(msg string, args ...interface{}) {
	success.Printf("  ✓ "+msg+"\n", args...)
}

// Warn prints a warning message in yellow.
func Warn(msg string, args ...interface{}) {
	warn.Printf("  ⚠ "+msg+"\n", args...)
}

// Error prints an error message in red.
func Error(msg string, args ...interface{}) {
	err_.Printf("  ✗ "+msg+"\n", args...)
}

// Bold prints bold text.
func Bold(msg string, args ...interface{}) {
	bold.Printf(msg, args...)
}

// BoldGreen prints bold green text.
func BoldGreen(msg string, args ...interface{}) {
	boldGreen.Printf(msg, args...)
}

// BoldRed prints bold red text.
func BoldRed(msg string, args ...interface{}) {
	boldRed.Printf(msg, args...)
}

// Dim prints dimmed text.
func Dim(msg string, args ...interface{}) {
	dim.Printf(msg, args...)
}
