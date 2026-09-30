//go:build !windows

package main

// fatalDialog is Windows-only; elsewhere the log says why Lumen stopped.
func fatalDialog(string) {}
