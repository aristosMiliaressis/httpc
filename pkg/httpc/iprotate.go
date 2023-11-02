package httpc

import (
	"net/url"

	"github.com/aristosMiliaressis/go-ip-rotate/pkg/iprotate"
	"github.com/aristosMiliaressis/httpc/internal/util"
)

// TODO: update request url for next requests
func (c *HttpClient) enableIpRotate(url *url.URL) error {
	var err error

	baseUrl := util.GetBaseUrl(url)

	c.errorMutex.Lock()

	if c.apiGateways[baseUrl.String()] != nil {
		return nil
	}

	c.apiGateways[baseUrl.String()], err = iprotate.CreateApi("default", baseUrl)
	if err != nil {
		// TODO: handle error? mark & stop trying
	}

	return err
}
