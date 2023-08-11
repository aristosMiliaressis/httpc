package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/aristosMiliaressis/httpc/pkg/httpc"
)

func main() {
	opts := httpc.DefaultOptions
	opts.ProxyUrl = "http://127.0.0.1:8080"

	client := httpc.NewHttpClient(opts, context.Background())

	// evt := client.SendRaw("GET https://example.com HTTP/1.1\r\nHost: google.com\r\n\r\n", "http://127.0.0.1:5000")
	// fmt.Println(evt.TransportError)

	req, _ := http.NewRequest("GET", "https://secure.spotlightpos.com", nil)
	req.Header.Add("Accept", "text/*")

	newOpts := client.Options
	newOpts.CacheBusting = httpc.AggressiveCacheBusting
	newOpts.CacheBusting.AcceptEncoding = true
	newOpts.CacheBusting.Origin = true
	newOpts.DefaultHeaders["Cache-Control"] = "no-transform"

	for {
		evt := client.SendWithOptions(req, &newOpts)
		fmt.Println(evt.TransportError)
		fmt.Println(evt.Response.StatusCode)

		body, err := ioutil.ReadAll(evt.Response.Body)
		if err != nil {
			fmt.Printf("Read Body Error %s\n", err)
		} else {
			fmt.Println(string(body))
		}
	}
}
