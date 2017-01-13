package main

import (
	"crypto/tls"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"time"

	pb "github.com/mehrdadrad/load/loadguide"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

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
	Error     string
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
	masterAddr  string
	slaveID     string
	lSlave, l   Load
	config      Config
	reqLoadChn  chan ReqLReply
	slaveResChn = make(chan Result, 100)
	shutdownChn = make(chan struct{}, 1)
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

func (s *server) SendSignal(cx context.Context, in *pb.Signal) (*pb.Empty, error) {
	for i := 0; i <= lSlave.workers; i++ {
		shutdownChn <- struct{}{}
	}
	log.Print("Interrupt requested")
	return &pb.Empty{}, nil
}

func (s *server) Ping(cx context.Context, in *pb.WhoAmI) (*pb.WhoAmI, error) {
	log.Print("Got ping from master")
	masterAddr = in.Laddr
	slaveID = in.Raddr
	go func() {
		var err error
		// send load request to master
		r, err := loadRequest(in.Laddr)
		if err != nil {
			// TODO
		}

		lSlave = Load{
			workers:  int(r.Workers),
			requests: int(r.Requests),
			urls:     r.Urls,
			isSlave:  true,
		}

		for _, url := range lSlave.urls {
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				continue
			}
			req.Header.Add("User-Agent", r.Useragent)
			lSlave.request = append(lSlave.request, req)
		}

		log.Printf("Ran slave : worker# %d, requests# %d", lSlave.workers, lSlave.requests)
		lSlave.Run()
	}()

	return &pb.WhoAmI{}, nil
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

		if workers == 0 {
			workers = 1
		}

		rRequests -= int(requests)
		rWorkers -= int(workers)

		l.hosts[i].requests = int(requests)

		h.reqLoad.respChn <- pb.LoadReply{
			Workers:   workers,
			Requests:  requests,
			Urls:      l.urls,
			Useragent: config.UserAgent,
		}
	}

	return rRequests, rWorkers
}

func (l *Load) gRPCServer() {
	hostPort := net.JoinHostPort(config.ListenBindAddr, config.Port)
	lis, err := net.Listen("tcp", hostPort)
	if err != nil {
		log.Print(err.Error())
		os.Exit(1)
	}
	defer lis.Close()
	grpcServer := grpc.NewServer()
	pb.RegisterLoadGuideServer(grpcServer, &server{})

	reflection.Register(grpcServer)
	if err := grpcServer.Serve(lis); err != nil {
		log.Print(err.Error())
		os.Exit(1)
	}
}

func (l *Load) ping(addr string) (*pb.WhoAmI, error) {
	hostPort := net.JoinHostPort(addr, config.Port)
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
	w := &pb.WhoAmI{Name: hostname, Laddr: lAddr, Raddr: addr}
	r, err := c.Ping(context.Background(), w)

	return r, err
}

func loadRequest(addr string) (*pb.LoadReply, error) {
	hostPort := net.JoinHostPort(addr, config.Port)
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

func sendSignal(addr string, signal int) (*pb.Empty, error) {
	hostPort := net.JoinHostPort(addr, config.Port)
	conn, err := grpc.Dial(hostPort, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	c := pb.NewLoadGuideClient(conn)
	request := &pb.Signal{int32(signal)}
	_, err = c.SendSignal(context.Background(), request)
	if err != nil {
		return nil, err
	}

	return &pb.Empty{}, nil
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

	if !l.isSlave {
		wDoneChan <- struct{}{}
	}

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

LOOP:
	for i := 0; i < n; i++ {
		for _, req := range l.request {
			select {
			case <-shutdownChn:
				break LOOP
			case resChan <- l.do(client, req):
			}
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
			URL:       req.URL.String(),
			Error:     err.Error(),
		}
	}

	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()

	return Result{
		Timestamp: int32(ts.Unix()),
		URL:       req.URL.String(),
		Status:    int32(resp.StatusCode),
		Error:     "",
		Trace:     trace,
	}
}

func (l *Load) resultProc(resChan chan Result, wDoneChan chan struct{}) {
	var (
		c          pb.LoadGuideClient
		counter    int
		slavesReq       = l.totalSlavesReq()
		progChn         = make(chan Result, 100)
		masterDone bool = l.isSlave
		slavesDone bool = false
	)

	if l.isSlave {
		hostPort := net.JoinHostPort(masterAddr, "9055")
		conn, err := grpc.Dial(hostPort, grpc.WithInsecure())
		if err != nil {
			log.Print(err.Error())
		}
		defer conn.Close()
		c = pb.NewLoadGuideClient(conn)
	}

	if slavesReq == 0 && !l.isSlave {
		slavesDone = true
	}

	go progress(l, progChn)

	for {
		if masterDone && slavesDone {
			break
		}
		select {
		case r := <-resChan:
			if l.isSlave {
				c.SendResult(context.Background(), &pb.LoadResMsg{
					ID:        slaveID,
					Url:       r.URL,
					Timestamp: r.Timestamp,
					Status:    r.Status,
				})
				if counter++; counter >= l.requests {
					slavesDone = true
				}
			} else {
				progChn <- r
			}
		case r := <-slaveResChn:
			progChn <- r
			if slavesReq--; slavesReq <= 0 {
				slavesDone = true
			}
		case <-shutdownChn:
			slavesDone = true
		case <-wDoneChan:
			masterDone = true
		}
	}

	time.Sleep(100 * time.Millisecond)
	close(progChn)
}

func (l *Load) totalSlavesReq() int {
	var total int
	for _, h := range l.hosts {
		total += h.requests
	}
	urlNum := len(l.urls)
	return total * urlNum
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

func (l *Load) gracefullStop() {
	close(shutdownChn)

	for _, h := range l.hosts {
		sendSignal(h.addr, 2)
	}
	if len(l.hosts) > 0 {
		log.Print("Sent interrupt signal to all slave(s)")
	}

	time.Sleep(2 * time.Second)
}

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

func quiet() bool {
	if config.Quiet {
		return true
	}
	return false
}

func (l *Load) sigHandler() {
	if l.isSlave {
		return
	}

	sigChn := make(chan os.Signal, 1)
	signal.Notify(sigChn, os.Interrupt)

	for {
		select {
		case <-sigChn:
			l.gracefullStop()
			log.Print("Interrupt requested")
			os.Exit(1)
		case <-time.Tick(1 * time.Second):
		}
	}
}

func NewLoad() Load {
	var (
		request []*http.Request
		hosts   []Host
	)

	for _, url := range config.Urls {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Add("User-Agent", config.UserAgent)
		request = append(request, req)
	}
	for _, host := range config.Hosts {
		hosts = append(hosts, Host{
			addr:   host,
			status: false,
		})
	}
	return Load{
		request:  request,
		requests: config.Requests,
		workers:  config.Workers,
		isSlave:  config.IsSlave,
		hosts:    hosts,
		timeout:  time.Duration(2) * time.Second,
	}
}

func init() {
	config = parseFlags()
	l = NewLoad()
	go l.sigHandler()
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

		select {
		case <-shutdownChn:
			time.Sleep(1 * time.Minute)
		default:
			log.Print("Test has been done")
		}
	}
}
