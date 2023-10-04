package httpc

import "net/http"

func (c *HttpClient) GetCookieJar() map[string]string {
	c.cookieJarMutex.RLock()
	defer c.cookieJarMutex.RUnlock()

	return c.cookieJar
}

func (c *HttpClient) AddCookie(name string, value string) {
	c.cookieJarMutex.Lock()
	if c.cookieJar[name] != value {
		c.cookieJar[name] = value
	}
	c.cookieJarMutex.Unlock()
}

func ContainsCookie(req *http.Request, cookieName string) bool {
	for _, cookie := range req.Cookies() {
		if cookie.Name == cookieName {
			return true
		}
	}
	return false
}
