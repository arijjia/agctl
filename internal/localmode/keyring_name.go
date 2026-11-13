package localmode

import "runtime"

func keyringName() string {
	switch runtime.GOOS {
	case "darwin":
		return "Keychain macOS"
	case "windows":
		return "Диспетчер учётных данных Windows"
	default:
		return "Secret Service"
	}
}
