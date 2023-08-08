package httpc

import (
	"net/url"
	"strings"
)

func ToAbsolute(src string, target string) string {

	srcUrl, _ := url.Parse(src)

	parsedUrl, err := url.Parse(target)

	if target == "" {
		target = srcUrl.String()
	} else if err != nil {
		target = ""
	} else if parsedUrl.IsAbs() || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "\\") {
		// keep absolute urls as is
		_ = target
	} else {
		// concatenate relative urls with base url

		if strings.HasPrefix(target, "/") {
			// root relative
			srcUrl.Path = ""
			srcUrl.RawQuery = ""
			target = srcUrl.String() + target
		} else {
			// current path relative
			if !strings.HasSuffix(srcUrl.String(), "/") {
				target = "/" + target
			}

			srcUrl.RawQuery = ""
			target = srcUrl.String() + target
		}
	}

	return target
}
