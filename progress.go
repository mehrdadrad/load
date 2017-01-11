package main

import (
	"fmt"
	_ "github.com/gosuri/uiprogress"
	_ "sync"
	_ "time"
)

func progress(hosts []Host, resChn chan Result) {
	//waitTime := time.Millisecond * 100
	//uiprogress.Start()

	//var wg sync.WaitGroup
	/*
		bar1 := uiprogress.AddBar(20).AppendCompleted().PrependElapsed()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for bar1.Incr() {
				time.Sleep(waitTime)
			}
		}()
	*/
	for {
		r, ok := <-resChn
		if !ok {
			break
		}
		fmt.Printf("%#v \n", r)
	}
}
