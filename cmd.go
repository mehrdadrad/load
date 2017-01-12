package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

type CMDURLs []string
type CMDHosts []string

func (i *CMDURLs) Set(value string) error {
	if _, err := url.ParseRequestURI(value); err != nil {
		return err
	}
	*i = append(*i, value)
	return nil
}

func (i *CMDURLs) String() string {
	return ""
}

func (i *CMDHosts) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func (i *CMDHosts) String() string {
	return ""
}

func parseFlags() Load {
	var (
		hosts CMDHosts
		urls  CMDURLs
		l     = Load{}
	)

	flag.Var(&hosts, "h", "slave hosts")
	flag.Var(&urls, "u", "URLs")
	flag.StringVar(&opt.port, "p", "9055", "port")
	flag.StringVar(&opt.mAddr, "b", "", "bind address")
	flag.BoolVar(&opt.quiet, "q", false, "quiet output")
	flag.BoolVar(&l.isSlave, "s", false, "slave mode")
	flag.IntVar(&l.requests, "r", 10, "requests")
	flag.IntVar(&l.workers, "c", 4, "concurrency")
	flag.Parse()

	for _, host := range hosts {
		l.hosts = append(l.hosts, Host{
			addr:   host,
			status: false,
		})
	}

	if len(urls) < 1 && !l.isSlave {
		log.Print("you need to specify at least an URL")
		os.Exit(2)
	}

	for _, url := range urls {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Add("User-Agent", "load")
		l.request = append(l.request, req)
	}

	l.urls = urls
	l.timeout = time.Duration(2) * time.Second

	return l
}

func flagUsage() string {
	// TODO
	return `

	`
}
