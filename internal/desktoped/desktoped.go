package desktoped

import (
	"os"
)

func FindBinary() string {
	paths := []string{
		"/usr/bin/desktopeditors",
		"/usr/local/bin/desktopeditors",
		"/opt/onlyoffice/desktopeditors/DesktopEditors",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
