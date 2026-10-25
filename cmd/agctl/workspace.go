// Legacy workspace-seat CLI. V3 creates projects inside the personal runtime;
// the separate workspace provisioning command is intentionally disabled.
package main

import (
	"fmt"
	"os"
)

func cmdWorkspace(args []string) {
	fmt.Fprintln(os.Stderr, "agctl workspace create демонтирована в v3: используйте персональный runtime и проекты-папки в Кабинете")
	os.Exit(2)
}
