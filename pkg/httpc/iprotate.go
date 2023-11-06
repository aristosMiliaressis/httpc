package httpc

import (
	"net/url"

	"github.com/aristosMiliaressis/go-ip-rotate/pkg/iprotate"
	"github.com/aristosMiliaressis/httpc/internal/util"
	"github.com/projectdiscovery/gologger"
)

func (c *HttpClient) enableIpRotate(url *url.URL) {
	var err error

	baseUrl := util.GetBaseUrl(url)

	c.apiGatewayMutex.Lock()
	defer c.apiGatewayMutex.Unlock()
	if c.apiGateways[baseUrl.String()] != nil {
		return
	}

	c.apiGateways[baseUrl.String()], err = iprotate.CreateApi("default", baseUrl)
	if err != nil {
		gologger.Fatal().Msgf("Error while creating api gateway for ip rotation")
	}
}
