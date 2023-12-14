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
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/gologger/levels"
)

func main() {
	gologger.DefaultLogger.SetMaxLevel(levels.LevelVerbose)

	opts := httpc.DefaultOptions
	opts.ErrorHandling.ReverseErrorCodeHandling = true

	client := httpc.NewHttpClient(opts, context.Background())
	defer client.Close()

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	req.Header.Add("Accept", "text/*")

	newOpts := client.Options
	client.ThreadPool.Rate.ChangeRate(4)

	var last *httpc.MessageDuplex
	for i := 0; i < 20; i++ {
		last = client.SendWithOptions(req, newOpts)
	}

	<-last.Resolved
	respData, _ := httputil.DumpResponse(last.Response, false)
	fmt.Println(string(respData))

	newOpts = client.Options
	client.ThreadPool.Rate.ChangeRate(30)

	go http.ListenAndServe("localhost:6060", nil)
	pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)

	for {
		_ = client.SendWithOptions(req.Clone(context.Background()), newOpts)
		req.Close = true
	}
}
