package main

import (
	"fmt"
	"net/http"

	"github.com/aristosMiliaressis/httpc/pkg/httpc"
)

func main() {
	opts := httpc.DefaultOptions

	client := httpc.NewHttpClient(opts)

	evt := client.SendRaw("GET https://example.com HTTP/1.1\r\nHost: google.com\r\n\r\n", "http://127.0.0.1:5000")
	fmt.Println(evt.TransportError)

	req, _ := http.NewRequest("GET", "http://127.0.0.1:5000/admin/test", nil)
	req.Header.Add("Accept", "text/*")

	newOpts := client.Options
	newOpts.ProxyUrl = "http://127.0.0.1:8080"
	newOpts.CacheBusting = httpc.AggressiveCacheBusting
	newOpts.CacheBusting.AcceptEncoding = true
	newOpts.CacheBusting.Origin = true
	newOpts.DefaultHeaders["Cache-Control"] = "no-transform"

	for {
		evt = client.SendWithOptions(req, &newOpts)
		fmt.Println(evt.Response.StatusCode)
	}
}
