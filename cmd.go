package main

import (
	"flag"
	_ "fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type URLs []string

func (i *URLs) Set(value string) error {
	if _, err := url.ParseRequestURI(value); err != nil {
		return err
	}
	*i = append(*i, value)
	return nil
}

func (i *URLs) String() string {
	return ""
}

func parseFlags() Load {
	var (
		hosts string
		urls  URLs
		l     = Load{}
	)

	flag.BoolVar(&l.isSlave, "s", false, "slave mode")
	flag.StringVar(&hosts, "h", "", "slave hosts")
	flag.Var(&urls, "u", "URLs")
	flag.StringVar(&ops.port, "p", "9055", "port")
	flag.IntVar(&l.requests, "r", 10, "requests")
	flag.IntVar(&l.workers, "c", 4, "concurrency")
	flag.Parse()

	if hosts != "" {
		for _, host := range strings.Split(hosts, ";") {
			l.hosts = append(l.hosts, Host{
				addr:   host,
				status: false,
			})
		}
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
