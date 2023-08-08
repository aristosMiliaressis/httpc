package main

import (
	"fmt"
	"net/http"

	"github.com/aristosMiliaressis/httpc"
)

func main() {
	opts := httpc.DefaultOptions
	opts.Proxy = "http://127.0.0.1:8080"

	client := httpc.NewHttpClient(opts)

	evt := client.SendRaw("GET https://example.com HTTP/1.1\r\nHost: google.com\r\n\r\n")

	newOpts := client.options
	req, _ := http.NewRequest("GET", "https://google.com", nil)

	evt = client.SendWithOptions(req, newOpts)

	fmt.Println(evt.Response.StatusCode)
}
