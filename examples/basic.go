package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	_ "net/http/pprof"
	"os"
	"runtime/pprof"

	"github.com/aristosMiliaressis/httpc/pkg/httpc"
)

func main() {
	opts := httpc.DefaultOptions
	//opts.ProxyUrl = "http://127.0.0.1:8080"
	ctx := context.Background()

	client := httpc.NewHttpClient(opts, ctx)
	defer client.Close()

	msg := client.SendRaw("GET /?cacheBuster=memRrNaqLRan HTTP/1.1\r\nHost: xxxx.h1-web-security-academy.net\r\nHost: apjDlpYjYRhR.example.com\r\nUser-Agent: Mozilla/5.0 (iPhone; CPU iPhone OS 16_5_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1\r\nAccept: */*, text/RQqfHFnOTfjw\r\nOrigin: https://HnjEAHZVtaUp.0ab300110358d490801b583100a100b8.h1-web-security-academy.net\r\nAccept-Encoding: gzip\r\n\r\n",
		"https://xxxx.h1-web-security-academy.net")
	respData, _ := httputil.DumpResponse(msg.Response, true)
	fmt.Println(string(respData))

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	req.Header.Add("Accept", "text/*")

	newOpts := client.Options
	client.ThreadPool.Rate.ChangeRate(1)

	var last *httpc.MessageDuplex
	for i := 0; i < 20; i++ {
		last = client.SendWithOptions(req, newOpts)
	}

	<-last.Resolved
	respData, _ = httputil.DumpResponse(last.Response, false)
	fmt.Println(string(respData))

	newOpts = client.Options
	newOpts.Timeout = 5
	//newOpts.ForceAttemptHTTP2 = true
	client.ThreadPool.Rate.ChangeRate(100)

	go http.ListenAndServe("localhost:6060", nil)
	pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)

	for {
		last = client.SendWithOptions(req.Clone(ctx), newOpts)
		req.Close = true
	}
}
