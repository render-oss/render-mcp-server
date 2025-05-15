package cfg

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// Parse Windows version from output like "Microsoft Windows [Version 10.0.19044.2604]"
var versionRegex = regexp.MustCompile(`\[Version ([.\d]+)`)

func getOSInfo() string {
	goos := runtime.GOOS
	var osName, osVersion string

	switch goos {
	case "windows":
		cmd := exec.Command("cmd", "ver")
		output, err := cmd.Output()
		if err == nil {
			if versionRegex.Match(output) {
				osVersion = versionRegex.FindStringSubmatch(string(output))[1]
			}
		}
		osName = "Windows"

	case "darwin":
		cmd := exec.Command("sw_vers", "-productVersion")
		output, err := cmd.Output()
		if err == nil {
			osVersion = strings.TrimSpace(string(output))
		}
		osName = "macOS"

	case "linux":
		// Try to get distribution info from /etc/os-release
		content, err := os.ReadFile("/etc/os-release")
		if err == nil {
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					osInfo := strings.Trim(line[12:], "\"")
					parts := strings.Fields(osInfo)
					if len(parts) > 0 {
						osName = parts[0]
						if len(parts) > 1 {
							osVersion = parts[1]
						}
					}
					break
				}
			}
		}
		if osName == "" {
			osName = "Linux"
		}

	default:
		osName = goos
	}

	if osVersion != "" {
		return fmt.Sprintf("%s - %s", osName, osVersion)
	}
	return osName
}
