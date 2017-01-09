package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptrace"
	"runtime"
	"strings"
	"sync"
	"time"

	pb "github.com/mehrdadrad/load/loadguide"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Options struct {
	isSlave bool
	urls    string
}

type Host struct {
	addr   string
	status bool
}

type Result struct {
	Timestamp time.Time
	URL       string
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
	request             []*http.Request
	requests            int
	workers             int
	rateLimit           int
	timeout             time.Duration
	hosts               []Host
	disabledTrace       bool
	disabledCompression bool
	disabledKeepAlive   bool
}

type ReqLReply struct {
	core    int32
	respChn chan pb.LoadReply
}

type server struct{}

var reqLoadChn chan ReqLReply

func (s *server) SendLoad(cx context.Context, in *pb.PowerRequest) (*pb.LoadReply, error) {
	// TODO
	println("GOT power!", in.Core)
	println("Reply workers!")
	var respChn = make(chan pb.LoadReply, 1)
	reqLoadChn <- ReqLReply{in.Core, respChn}
	loadReply := <-respChn
	return &loadReply, nil
}

func (s *server) Ping(cx context.Context, in *pb.WhoAmI) (*pb.WhoAmI, error) {
	println("Got Ping")
	// TODO: ask load
	go func() {
		// send load request
		w, r, err := loadRequest()
		if err != nil {

		}
		println("run load test w/", w, r)
	}()
	return &pb.WhoAmI{}, nil
}

func NewTest() (Load, error) {
	var urls = []string{"https://www.google.com", "https://www.freebsd.org"}
	l := Load{}
	for _, url := range urls {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Add("User-Agent", "load")
		l.request = append(l.request, req)
	}
	l.timeout = time.Duration(2) * time.Second
	l.requests = 10001
	l.workers = 19

	return l, nil
}

func (l *Load) loadBalance() (int, int) {
	var (
		totalCores int32 = int32(runtime.NumCPU())
		rRequests  int   = l.requests
		rWorkers   int   = l.workers
		tmp        []ReqLReply
	)
	for range l.hosts {
		lc := <-reqLoadChn
		totalCores += lc.core
		tmp = append(tmp, lc)
	}
	println(totalCores)
	for _, h := range tmp {
		pct := (h.core * 100) / totalCores
		requests := pct * int32(l.requests) / 100
		workers := pct * int32(l.workers) / 100
		rRequests -= int(requests)
		rWorkers -= int(workers)
		h.respChn <- pb.LoadReply{Workers: workers, Requests: requests}
	}

	return rRequests, rWorkers
}

func (l *Load) gRPCServer() error {
	lis, err := net.Listen("tcp", ":9055")
	if err != nil {
		return err
	}
	defer lis.Close()
	grpcServer := grpc.NewServer()
	pb.RegisterLoadGuideServer(grpcServer, &server{})

	reflection.Register(grpcServer)
	if err := grpcServer.Serve(lis); err != nil {
		return err
	}

	return nil
}

func (l *Load) ping(addr string) (*pb.WhoAmI, error) {
	hostPort := net.JoinHostPort(addr, "9055")
	conn, err := grpc.Dial(hostPort, grpc.WithInsecure())
	if err != nil {
		return &pb.WhoAmI{}, err
	}
	defer conn.Close()

	c := pb.NewLoadGuideClient(conn)
	w := &pb.WhoAmI{}
	r, err := c.Ping(context.Background(), w)

	return r, err
}

func loadRequest() (int, int, error) {
	conn, err := grpc.Dial("localhost:9055", grpc.WithInsecure())
	if err != nil {
		return 0, 0, nil
	}
	defer conn.Close()

	c := pb.NewLoadGuideClient(conn)
	request := &pb.PowerRequest{
		Core: int32(runtime.NumCPU()),
	}
	r, err := c.SendLoad(context.Background(), request)
	if err != nil {
		return 0, 0, nil
	}

	return int(r.Workers), int(r.Requests), nil
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
		for _, req := range l.request {
			resChan <- l.do(client, req)
		}
	}
}

func (l *Load) do(client *http.Client, request *http.Request) Result {
	var (
		req   = new(http.Request)
		trace Trace
	)

	*req = *request

	if !l.disabledTrace {
		ctx := httptrace.WithClientTrace(req.Context(), tracer(&trace))
		req = req.WithContext(ctx)
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
		URL:       req.URL.String(),
		Status:    resp.StatusCode,
		Error:     nil,
		Trace:     trace,
	}
}

func (l *Load) resultProc(resChan chan Result, wDoneChan chan struct{}) {
LOOP:
	for {
		select {
		case r := <-resChan:
			fmt.Printf("%#v \n", r)
		case <-wDoneChan:
			break LOOP
		}
	}
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

func (l *Load) chkHosts(hosts []Host) bool {
	var allUp bool = true
	for i := range hosts {
		_, err := l.ping(hosts[i].addr)
		if err != nil {
			allUp = false
			println(hosts[i].addr, "is unreachable")
		} else {
			hosts[i].status = true
			println(hosts[i].addr, "is up")
		}
	}
	return allUp
}

var (
	ops Options
	l   Load
)

func init() {
	var hosts string
	flag.BoolVar(&ops.isSlave, "s", false, "slave mode")
	flag.StringVar(&hosts, "h", "", "slave hosts")
	flag.StringVar(&ops.urls, "u", "", "URLs")
	flag.Parse()

	l, _ = NewTest()

	if hosts != "" {
		for _, host := range strings.Split(hosts, ";") {
			l.hosts = append(l.hosts, Host{host, false})
		}
	}

	reqLoadChn = make(chan ReqLReply, len(l.hosts)+1)
}

func main() {
	println(ops.isSlave)

	if ops.isSlave {
		l.gRPCServer()
	} else {
		go l.gRPCServer()
		if len(l.hosts) > 0 {
			if !l.chkHosts(l.hosts) {
				return
			} else {
				r, w := l.loadBalance()
				println("run master :", w, r)
			}
		}
	}

	time.Sleep(5 * time.Second)
	/*
		l, err := NewTest()
		if err != nil {
			println(err.Error())
			return
		}
		l.Run()
	*/
}
