package httpc

import (
	"fmt"
	"net/http"
	"strings"
)

var secFetchDestMap = map[string]string{
	"js":    "script",
	"jsm":   "script",
	"css":   "style",
	"ico":   "image",
	"svg":   "image",
	"png":   "image",
	"jpg":   "image",
	"jpeg":  "image",
	"gif":   "image",
	"webp":  "image",
	"woff":  "font",
	"woff2": "font",
	"otf":   "font",
	"ttf":   "font",
	"mp4":   "video",
	"mov":   "video",
	"wmv":   "video",
	"avi":   "video",
	"webm":  "video",
	"mp3":   "audio",
}

func simulateBrowserRequest(req *http.Request) {

	extensions := make([]string, len(secFetchDestMap))

	i := 0
	for k := range secFetchDestMap {
		extensions[i] = k
		i++
	}

	segments := strings.Split(req.URL.Path, ".")
	if len(segments) > 0 && Contains(extensions, segments[len(segments)-1]) {
		addHeaderIfNotPresent(req, "Sec-Ch-Ua", "\"Chromium\";v=\"117\", \"Not;A=Brand\";v=\"8\"")
		addHeaderIfNotPresent(req, "Sec-Ch-Ua-Mobile", "?0")
		addHeaderIfNotPresent(req, "User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.5938.132 Safari/537.36")
		addHeaderIfNotPresent(req, "Sec-Ch-Ua-Platform", "\"Windows\"")
		addHeaderIfNotPresent(req, "Accept", "*/*;q=0.9")
		addHeaderIfNotPresent(req, "Sec-Fetch-Site", "same-origin")
		addHeaderIfNotPresent(req, "Sec-Fetch-Mode", "no-cors")
		addHeaderIfNotPresent(req, "Sec-Fetch-Dest", secFetchDestMap[segments[len(segments)-1]])
		addHeaderIfNotPresent(req, "Referer", fmt.Sprintf("%s://%s/", req.URL.Scheme, req.Host))
		addHeaderIfNotPresent(req, "Accept-Encoding", "gzip, deflate, br")
		addHeaderIfNotPresent(req, "Accept-Language", "en-US,en;q=0.9")
		return
	}

	addHeaderIfNotPresent(req, "Sec-Ch-Ua", "\"Chromium\";v=\"117\", \"Not;A=Brand\";v=\"8\"")
	addHeaderIfNotPresent(req, "Sec-Ch-Ua-Mobile", "?0")
	addHeaderIfNotPresent(req, "Sec-Ch-Ua-Platform", "\"Windows\"")
	addHeaderIfNotPresent(req, "Upgrade-Insecure-Requests", "1")
	addHeaderIfNotPresent(req, "User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.5938.132 Safari/537.36")
	addHeaderIfNotPresent(req, "Accept", "*/*;q=0.9")
	addHeaderIfNotPresent(req, "Sec-Fetch-Site", "none")
	addHeaderIfNotPresent(req, "Sec-Fetch-Mode", "navigate")
	addHeaderIfNotPresent(req, "Sec-Fetch-User", "?1")
	addHeaderIfNotPresent(req, "Sec-Fetch-Dest", "document")
	addHeaderIfNotPresent(req, "Accept-Encoding", "gzip, deflate, br")
	addHeaderIfNotPresent(req, "Accept-Language", "en-US,en;q=0.9")
}

func addHeaderIfNotPresent(req *http.Request, name, value string) {
	if req.Header.Get(name) == "" {
		addHeaderIfNotPresent(req, name, value)
	}
}
