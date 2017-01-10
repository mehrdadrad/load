package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
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
	urls string
	port string
}

type Host struct {
	addr     string
	requests int
	status   bool
	reqLoad  ReqLReply
}

type Result struct {
	ID        string
	Timestamp int32
	URL       string
	Status    int32
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
	urls                []string
	requests            int
	workers             int
	rateLimit           int
	timeout             time.Duration
	hosts               []Host
	isSlave             bool
	disabledTrace       bool
	disabledCompression bool
	disabledKeepAlive   bool
}

type ReqLReply struct {
	core    int32
	respChn chan pb.LoadReply
}

type server struct{}

var (
	reqLoadChn  chan ReqLReply
	slaveResChn = make(chan Result, 100)
)

func (s *server) SendResult(cx context.Context, in *pb.LoadResMsg) (*pb.Empty, error) {
	slaveResChn <- Result{
		ID:        in.ID,
		Status:    in.Status,
		URL:       in.Url,
		Timestamp: in.Timestamp,
	}
	return &pb.Empty{}, nil
}

func (s *server) SendLoad(cx context.Context, in *pb.PowerRequest) (*pb.LoadReply, error) {
	var respChn = make(chan pb.LoadReply, 1)
	reqLoadChn <- ReqLReply{in.Core, respChn}
	loadReply := <-respChn
	return &loadReply, nil
}

func (s *server) Ping(cx context.Context, in *pb.WhoAmI) (*pb.WhoAmI, error) {
	log.Print("Got ping from master")
	go func() {
		var err error
		// send load request to master
		r, err := loadRequest(in.Addr)
		if err != nil {

		}
		lSlave, _ := NewTest()
		lSlave.workers = int(r.Workers)
		lSlave.requests = int(r.Requests)
		lSlave.urls = r.Urls
		lSlave.isSlave = true
		log.Printf("Ran slave : worker# %d, requests# %d", lSlave.workers, lSlave.requests)
		lSlave.Run()
	}()
	return &pb.WhoAmI{}, nil
}

func NewTest() (Load, error) {
	l := Load{}
	l.urls = []string{"https://www.google.com", "https://www.freebsd.org"}
	for _, url := range l.urls {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Add("User-Agent", "load")
		l.request = append(l.request, req)
	}
	l.timeout = time.Duration(2) * time.Second

	return l, nil
}

func (l *Load) loadBalance() (int, int) {
	var (
		totalCores int32 = int32(runtime.NumCPU())
		rRequests  int   = l.requests
		rWorkers   int   = l.workers
	)

	for i, _ := range l.hosts {
		lc := <-reqLoadChn
		totalCores += lc.core
		l.hosts[i].reqLoad = lc
	}

	for i, h := range l.hosts {
		pct := (h.reqLoad.core * 100) / totalCores
		requests := pct * int32(l.requests) / 100
		workers := pct * int32(l.workers) / 100
		rRequests -= int(requests)
		rWorkers -= int(workers)

		l.hosts[i].requests = int(requests)

		h.reqLoad.respChn <- pb.LoadReply{Workers: workers, Requests: requests, Urls: l.urls}
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

	lAddr, err := getLocalAddr(addr)
	if err != nil {
		return &pb.WhoAmI{}, err
	}

	hostname, _ := os.Hostname()
	c := pb.NewLoadGuideClient(conn)
	w := &pb.WhoAmI{Name: hostname, Addr: lAddr}
	r, err := c.Ping(context.Background(), w)

	return r, err
}

func loadRequest(addr string) (*pb.LoadReply, error) {
	hostPort := net.JoinHostPort(addr, "9055")
	conn, err := grpc.Dial(hostPort, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	c := pb.NewLoadGuideClient(conn)
	request := &pb.PowerRequest{
		Core: int32(runtime.NumCPU()),
	}
	r, err := c.SendLoad(context.Background(), request)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func sendResult(addr string) (*pb.LoadReply, error) {
	hostPort := net.JoinHostPort(addr, "9055")
	conn, err := grpc.Dial(hostPort, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	c := pb.NewLoadGuideClient(conn)
	request := &pb.PowerRequest{
		Core: int32(runtime.NumCPU()),
	}
	r, err := c.SendLoad(context.Background(), request)
	if err != nil {
		return nil, err
	}

	return r, nil
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

	for c := 1; c <= l.workers; c++ {
		go func(c int) {
			reqPerWorker := l.requests / l.workers
			defer wg0.Done()
			if c != l.workers {
				l.worker(reqPerWorker, resChan)
			} else {
				reqPerWorker = l.requests - (reqPerWorker * (l.workers - 1))
				l.worker(reqPerWorker, resChan)
			}
		}(c)
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
			Timestamp: int32(ts.Unix()),
			Error:     err,
		}
	}

	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()

	return Result{
		Timestamp: int32(ts.Unix()),
		URL:       req.URL.String(),
		Status:    int32(resp.StatusCode),
		Error:     nil,
		Trace:     trace,
	}
}

func (l *Load) resultProc(resChan chan Result, wDoneChan chan struct{}) {
	var (
		c           pb.LoadGuideClient
		hostname, _ = os.Hostname()
	)

	if l.isSlave {
		hostPort := net.JoinHostPort("localhost", "9055")
		conn, err := grpc.Dial(hostPort, grpc.WithInsecure())
		if err != nil {
			println("ERROR:", err.Error())
		}
		defer conn.Close()
		c = pb.NewLoadGuideClient(conn)
	}

LOOP:
	for {
		select {
		case r := <-resChan:
			if l.isSlave {
				c.SendResult(context.Background(), &pb.LoadResMsg{
					ID:        hostname,
					Url:       r.URL,
					Timestamp: r.Timestamp,
					Status:    r.Status,
				})
			} else {
				fmt.Printf("MASTER: %#v \n", r.Status)
			}
		case r := <-slaveResChn:
			_ = r
			fmt.Printf("SLAVE: %#v \n", r.Status)
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
			log.Print(hosts[i].addr, " is unreachable")
		} else {
			hosts[i].status = true
			log.Print(hosts[i].addr, " is up")
		}
	}
	return allUp
}

var (
	ops Options
	l   Load
)

func getLocalAddr(rAddr string) (string, error) {
	hostPort := net.JoinHostPort(rAddr, "9055")
	conn, err := net.Dial("tcp", hostPort)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return "", err
	}

	return host, nil
}

func init() {
	var hosts string

	l, _ = NewTest()

	flag.BoolVar(&l.isSlave, "s", false, "slave mode")
	flag.StringVar(&hosts, "h", "", "slave hosts")
	flag.StringVar(&ops.urls, "u", "", "URLs")
	flag.StringVar(&ops.port, "p", "9055", "port")
	flag.IntVar(&l.requests, "r", 10, "port")
	flag.IntVar(&l.workers, "w", 4, "port")
	flag.Parse()

	if hosts != "" {
		for _, host := range strings.Split(hosts, ";") {
			l.hosts = append(l.hosts, Host{
				addr:   host,
				status: false,
			})
		}
	}

	reqLoadChn = make(chan ReqLReply, len(l.hosts)+1)
}

func main() {
	if l.isSlave {
		log.Print("Starting... as slave")
		l.gRPCServer()
	} else {
		log.Print("Starting... as master")
		lMaster := l
		go l.gRPCServer()
		if len(l.hosts) > 0 {
			if !l.chkHosts(l.hosts) {
				return
			} else {
				lMaster.requests, lMaster.workers = l.loadBalance()
			}
		}
		log.Printf("Ran master : worker# %d, requests# %d", lMaster.workers, lMaster.requests)
		lMaster.Run()
		fmt.Printf("%#v\n", l.hosts)
	}
}
