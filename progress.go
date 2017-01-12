package main

import (
	"fmt"
	"github.com/gosuri/uiprogress"
	"github.com/gosuri/uiprogress/util/strutil"
)

func progress(l *Load, resChn chan Result) {
	var (
		bars = make(map[string]*uiprogress.Bar)
		p    = uiprogress.New()
	)

	if quiet() || l.isSlave {
		return
	}

	p.Start()

	for _, h := range l.hosts {
		bars[h.addr] = p.AddBar(h.requests).AppendCompleted().PrependElapsed()
		go func(h Host) {
			bars[h.addr].PrependFunc(func(b *uiprogress.Bar) string {
				return strutil.Resize(fmt.Sprintf("Slave (%s)", h.addr), 25)
			})
		}(h)
	}

	bars[""] = p.AddBar(l.requests).AppendCompleted().PrependElapsed()
	bars[""].PrependFunc(func(b *uiprogress.Bar) string {
		return strutil.Resize(fmt.Sprintf("Master"), 25)
	})

	for {
		r, ok := <-resChn
		if !ok {
			break
		}
		bars[r.ID].Incr()
	}

	p.Stop()
}
