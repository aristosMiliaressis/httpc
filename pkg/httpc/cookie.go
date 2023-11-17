package httpc

import "net/http"

func (c *HttpClient) GetCookieJar() map[string]string {
	c.cookieJarMutex.RLock()
	defer c.cookieJarMutex.RUnlock()

	jarCopy := c.cookieJar

	return jarCopy
}

func (c *HttpClient) AddCookie(name string, value string) {
	c.cookieJarMutex.Lock()
	defer c.cookieJarMutex.Unlock()

	if c.cookieJar[name] != value {
		c.cookieJar[name] = value
	}
}

func ContainsCookie(req *http.Request, cookieName string) bool {
	for _, cookie := range req.Cookies() {
		if cookie.Name == cookieName {
			return true
		}
	}
	return false
}
