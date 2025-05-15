package cfg

import (
	"fmt"
	"net/http"
	"os"
)

const RepoURL = "https://api.github.com/repos/render-oss/cli"
const InstallationInstructionsURL = "https://render.com/docs/cli#1-install"

var Version = "dev"
var osInfo string

func GetHost() string {
	if host := os.Getenv("RENDER_HOST"); host != "" {
		return host
	}

	return "https://api.render.com/v1/"
}

func GetAPIKey() string {
	return os.Getenv("RENDER_API_KEY")
}

func AddUserAgent(header http.Header) http.Header {
	header.Add("user-agent", fmt.Sprintf("render-cli/%s (%s)", Version, getOSInfoOnce()))
	return header
}

func getOSInfoOnce() string {
	if osInfo == "" {
		osInfo = getOSInfo()
	}
	return osInfo
}
