package cfg

import (
	"fmt"
	"net/http"
	"os"
)

var Version = "dev"
var osInfo string

func GetHost() string {
	baseHost := "api.render.com"
	if host := os.Getenv("RENDER_HOST"); host != "" {
		baseHost = host
	}
	return fmt.Sprintf("https://%s/v1", baseHost)
}

func GetAPIKey() string {
	return os.Getenv("RENDER_API_KEY")
}

func AddUserAgent(header http.Header) http.Header {
	header.Add("user-agent", fmt.Sprintf("render-mcp-server/%s (%s)", Version, getOSInfoOnce()))
	return header
}

func getOSInfoOnce() string {
	if osInfo == "" {
		osInfo = getOSInfo()
	}
	return osInfo
}
