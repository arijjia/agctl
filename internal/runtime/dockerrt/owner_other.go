//go:build !unix

package dockerrt

// foreignOwned — на Windows владельцев bind-маунтов переводит Docker Desktop.
func foreignOwned(string, int) bool { return false }
