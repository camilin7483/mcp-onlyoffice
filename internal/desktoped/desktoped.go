package desktoped

import (
	"os"
)

func FindBinary() string {
	paths := []string{
		"/opt/onlyoffice/desktopeditors/DesktopEditors",
		"/usr/bin/desktopeditors",
		"/usr/local/bin/desktopeditors",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
