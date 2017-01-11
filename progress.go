package main

import (
	"fmt"
	"github.com/gosuri/uiprogress"
	_ "sync"
	_ "time"
)

func progress(l *Load, resChn chan Result) {
	var (
		bar            = make(map[string]*uiprogress.Bar)
		slavesRequests int
	)

	for _, h := range l.hosts {
		fmt.Printf("%#v \n", h)
		bar[h.addr] = uiprogress.AddBar(h.requests).AppendCompleted().PrependElapsed()
		bar[h.addr].PrependFunc(func(b *uiprogress.Bar) string {
			return fmt.Sprintf("Slave  ")
		})
		slavesRequests += h.requests
	}

	bar[""] = uiprogress.AddBar(l.requests - slavesRequests).AppendCompleted().PrependElapsed()
	bar[""].PrependFunc(func(b *uiprogress.Bar) string {
		return fmt.Sprintf("Master ")
	})

	uiprogress.Start()

	for {
		r, ok := <-resChn
		if !ok {
			break
		}
		bar[r.ID].Incr()
	}
}
