package main

import (
	"crypto/tls"
	_ "fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptrace"
	"time"

	"sync"
)

type Result struct {
	Timestamp time.Time
	Status    int
	Error     error
	Trace     Trace
}

type Trace struct {
	ConnectionStart float64
	ConnectionTime  float64
	TimeToFirstByte float64
}

type Load struct {
	request             *http.Request
	requests            int
	workers             int
	rateLimit           int
	timeout             time.Duration
	disabledTrace       bool
	disabledCompression bool
	disabledKeepAlive   bool
}

func NewTest() (Load, error) {
	var err error
	l := Load{}
	l.request, err = http.NewRequest("GET", "https://www.google.com", nil)
	l.request.Header.Add("User-Agent", "load")
	l.timeout = time.Duration(2) * time.Second
	l.requests = 10
	l.workers = 2
	if err != nil {
		return l, err
	}

	return l, nil
}

func (l *Load) Run() {
	var (
		wg0, wg1  sync.WaitGroup
		wDoneChan = make(chan struct{}, 1)
		resChan   = make(chan Result, 100)
	)

	wg1.Add(1)
	go func() {
		defer wg1.Done()
		l.resultProc(resChan, wDoneChan)
	}()

	wg0.Add(l.workers)
	for c := 0; c < l.workers; c++ {
		go func() {
			defer wg0.Done()
			l.worker(l.requests/l.workers, resChan)
		}()
	}

	wg0.Wait()
	wDoneChan <- struct{}{}
	wg1.Wait()
}

func (l *Load) worker(n int, resChan chan Result) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DisableCompression: true,
		DisableKeepAlives:  true,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   l.timeout,
	}
	for i := 0; i < n; i++ {
		resChan <- l.do(client)
	}
}

func (l *Load) do(client *http.Client) Result {
	var (
		req   = new(http.Request)
		trace Trace
	)

	*req = *l.request
	if !l.disabledTrace {
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), tracer(&trace)))
	}
	ts := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Result{
			Timestamp: ts,
			Error:     err,
		}
	}

	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()

	return Result{
		Timestamp: ts,
		Status:    resp.StatusCode,
		Error:     nil,
		Trace:     trace,
	}
}

func (l *Load) resultProc(resChan chan Result, wDoneChan chan struct{}) {

}

func tracer(t *Trace) *httptrace.ClientTrace {
	var (
		start   = time.Now()
		elapsed time.Duration
	)

	return &httptrace.ClientTrace{
		ConnectStart: func(network, addr string) {
			elapsed = time.Since(start)
			start = time.Now()
			t.ConnectionStart = elapsed.Seconds() * 1e3
		},
		ConnectDone: func(network, addr string, err error) {
			elapsed = time.Since(start)
			start = time.Now()
			t.ConnectionTime = elapsed.Seconds() * 1e3
		},
		GotFirstResponseByte: func() {
			elapsed = time.Since(start)
			start = time.Now()
			t.TimeToFirstByte = elapsed.Seconds() * 1e3
		},
	}
}

func main() {
	l, err := NewTest()
	if err != nil {
		println(err.Error())
		return
	}
	l.Run()
}
