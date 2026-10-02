//go:build !windows

package main

// fatalDialog is Windows-only; elsewhere the log says why Lumen stopped.
func fatalDialog(string) {}

// lowerPriority and raiseWebView are Windows-only for now.
func lowerPriority() {}

func raiseWebView() {}
