package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"

	"github.com/aristosMiliaressis/httpc/pkg/httpc"
)

func main() {
	opts := httpc.DefaultOptions
	opts.ProxyUrl = "http://127.0.0.1:8080"

	client := httpc.NewHttpClient(opts, context.Background())
	defer client.Close()

	msg := client.SendRaw("GET /?cacheBuster=memRrNaqLRan HTTP/1.1\r\nHost: xxxx.h1-web-security-academy.net\r\nHost: apjDlpYjYRhR.example.com\r\nUser-Agent: Mozilla/5.0 (iPhone; CPU iPhone OS 16_5_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1\r\nAccept: */*, text/RQqfHFnOTfjw\r\nOrigin: https://HnjEAHZVtaUp.0ab300110358d490801b583100a100b8.h1-web-security-academy.net\r\nAccept-Encoding: gzip\r\n\r\n",
		"https://xxxx.h1-web-security-academy.net")
	respData, _ := httputil.DumpResponse(msg.Response, true)
	fmt.Println(string(respData))

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	req.Header.Add("Accept", "text/*")

	newOpts := client.Options
	newOpts.DebugLogging = true
	newOpts.MaxThreads = 6
	newOpts.CacheBusting = httpc.AggressiveCacheBusting
	newOpts.CacheBusting.AcceptEncoding = true
	newOpts.CacheBusting.Origin = true
	newOpts.DefaultHeaders["Cache-Control"] = "no-transform"

	var last *httpc.MessageDuplex
	for i := 0; i < 20; i++ {
		last = client.SendWithOptions(req, newOpts)
	}

	<-last.Resolved
	respData, _ = httputil.DumpResponse(last.Response, false)
	fmt.Println(string(respData))
}
