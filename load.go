package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"sync"
)

type Load struct {
	request             *http.Request
	requests            int
	workers             int
	rateLimit           int
	disabledCompression bool
	disabledKeepAlive   bool
}

func NewTest() (Load, error) {
	var err error
	l := Load{}
	l.request, err = http.NewRequest("GET", "https://google.com", nil)
	l.request.Header.Add("User-Agent", "load")
	l.requests = 1000
	l.workers = 50
	if err != nil {
		return l, err
	}

	return l, nil
}

func (l *Load) Run() {
	var wg sync.WaitGroup
	wg.Add(l.workers)
	for c := 0; c < l.workers; c++ {
		go func() {
			defer wg.Done()
			l.worker(l.requests / l.workers)
		}()
	}
	wg.Wait()
}

func (l *Load) worker(n int) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DisableCompression: true,
		DisableKeepAlives:  true,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   time.Duration(2) * time.Second,
	}
	for i := 0; i < n; i++ {
		l.do(client)
		fmt.Printf("!")
	}
}

func (l *Load) do(client *http.Client) {
	var req = new(http.Request)
	*req = *l.request
	resp, err := client.Do(req)
	if err != nil {
		println(err.Error())
		return
	}
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
}

func main() {
	l, err := NewTest()
	if err != nil {
		println(err.Error())
		return
	}
	l.Run()
}
